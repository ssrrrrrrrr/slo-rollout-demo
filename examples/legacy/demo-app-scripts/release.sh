#!/bin/bash
set -euo pipefail

if [ $# -ne 4 ]; then
  echo "Usage: $0 <IMAGE_TAG> <APP_VERSION> <FAULT_RATE> <LATENCY_MS>"
  echo "Example healthy: $0 v1-ci3 v1 0 0"
  echo "Example bad:     $0 v2-ci-bad v2 0.8 800"
  exit 1
fi

IMAGE_TAG="$1"
APP_VERSION="$2"
FAULT_RATE="$3"
LATENCY_MS="$4"

REGISTRY="registry.local:5000"
IMAGE_NAME="sre/demo-app"
NAMESPACE="slo-rollout"
ROLLOUT_NAME="demo-app"

LOCAL_IMAGE="docker.io/library/demo-app:${IMAGE_TAG}"
REMOTE_IMAGE="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
TAR_FILE="demo-app-${IMAGE_TAG}.tar"

echo "========== Release Info =========="
echo "IMAGE_TAG:   ${IMAGE_TAG}"
echo "APP_VERSION: ${APP_VERSION}"
echo "FAULT_RATE:  ${FAULT_RATE}"
echo "LATENCY_MS:  ${LATENCY_MS}"
echo "REMOTE:      ${REMOTE_IMAGE}"
echo "=================================="

echo "[1/7] Build Go binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o demo-app main.go

echo "[2/7] Build image tar..."
python3 build-image.py "${IMAGE_TAG}" "${APP_VERSION}" "${FAULT_RATE}" "${LATENCY_MS}"

echo "[3/7] Import image into containerd..."
ctr -n k8s.io images import "${TAR_FILE}"

echo "[4/7] Tag image for private registry..."
ctr -n k8s.io images tag --force "${LOCAL_IMAGE}" "${REMOTE_IMAGE}"

echo "[5/7] Push image to private registry..."
ctr -n k8s.io images push --plain-http "${REMOTE_IMAGE}"

echo "[6/7] Update AnalysisTemplate for current canary version..."
cat > demo-app-analysis.yaml <<YAML
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: demo-app-error-rate
  namespace: ${NAMESPACE}
spec:
  metrics:
    - name: error-rate
      interval: 20s
      count: 3
      failureLimit: 1
      successCondition: result[0] < 5
      provider:
        prometheus:
          address: http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090
          query: |
            (
              (
                sum(rate(demo_http_requests_total{namespace="${NAMESPACE}",version="${IMAGE_TAG}",status=~"5.."}[1m]))
                or vector(0)
              )
              /
              clamp_min(
                (
                  sum(rate(demo_http_requests_total{namespace="${NAMESPACE}",version="${IMAGE_TAG}"}[1m]))
                  or vector(0)
                ),
                0.001
              )
            ) * 100

    - name: p95-latency
      interval: 20s
      count: 3
      failureLimit: 1
      successCondition: isNaN(result[0]) || result[0] < 0.3
      provider:
        prometheus:
          address: http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090
          query: |
            (
              histogram_quantile(
                0.95,
                sum(rate(demo_http_request_duration_seconds_bucket{namespace="${NAMESPACE}",version="${IMAGE_TAG}"}[1m])) by (le)
              )
              or on() vector(0)
            )
YAML

kubectl apply -f demo-app-analysis.yaml

echo "[7/7] Patch Argo Rollout..."
kubectl -n "${NAMESPACE}" patch rollout "${ROLLOUT_NAME}" --type=json -p="[
  {\"op\":\"replace\",\"path\":\"/spec/template/metadata/labels/version\",\"value\":\"${IMAGE_TAG}\"},
  {\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/image\",\"value\":\"${REMOTE_IMAGE}\"},
  {\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/env\",\"value\":[
    {\"name\":\"VERSION\",\"value\":\"${APP_VERSION}\"},
    {\"name\":\"RELEASE_TAG\",\"value\":\"${IMAGE_TAG}\"},
    {\"name\":\"FAULT_RATE\",\"value\":\"${FAULT_RATE}\"},
    {\"name\":\"LATENCY_MS\",\"value\":\"${LATENCY_MS}\"}
  ]}
]"

echo
echo "Release submitted."
echo
echo "Check rollout:"
echo "  kubectl get rollout -n ${NAMESPACE}"
echo "  kubectl get analysisrun -n ${NAMESPACE}"
echo "  kubectl get pods -n ${NAMESPACE} -L version -o wide"
echo
echo "Check registry tags:"
echo "  curl http://${REGISTRY}/v2/${IMAGE_NAME}/tags/list"
