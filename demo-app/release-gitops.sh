#!/bin/bash
set -euo pipefail

if [ $# -ne 4 ]; then
  echo "Usage: $0 <image_tag> <app_version> <fault_rate> <latency_ms>"
  echo "Example healthy: $0 v1-actions v1 0 0"
  echo "Example bad:     $0 v2-actions-bad v2 0.8 800"
  exit 1
fi

IMAGE_TAG="$1"
APP_VERSION="$2"
FAULT_RATE="$3"
LATENCY_MS="$4"

S_SENTINEL_ENV="${S_SENTINEL_ENV:-dev}"

REGISTRY="registry.local:5000"
IMAGE_NAME="sre/demo-app"
NAMESPACE="slo-rollout"
ROLLOUT_NAME="demo-app"

ROOT_DIR="$(git rev-parse --show-toplevel)"
APP_DIR="${ROOT_DIR}/demo-app"
DEPLOY_DIR="${ROOT_DIR}/deploy"
BASE_DIR="${ROOT_DIR}/deploy/base"

LOCAL_IMAGE="docker.io/library/demo-app:${IMAGE_TAG}"
REMOTE_IMAGE="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"
TAR_FILE="demo-app-${IMAGE_TAG}.tar"

SLO_CONFIG_FILE="${ROOT_DIR}/configs/services/demo-app.slo.yaml"
SLO_ERROR_RATE_THRESHOLD="$(python3 - "$SLO_CONFIG_FILE" <<'PY'
import sys
import yaml
data = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
for obj in data["spec"]["objectives"]:
    if obj["id"] == "error-rate":
        print(obj["threshold"]["value"])
        break
PY
)"
SLO_P95_SECONDS_THRESHOLD="$(python3 - "$SLO_CONFIG_FILE" <<'PY'
import sys
import yaml
data = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
for obj in data["spec"]["objectives"]:
    if obj["id"] == "p95-latency":
        print(obj["threshold"]["value"])
        break
PY
)"
SLO_MIN_REQUEST_COUNT="$(python3 - "$SLO_CONFIG_FILE" <<'PY'
import sys
import yaml
data = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))
print(data["spec"]["evaluation"]["minRequestCount"])
PY
)"

echo "========== GitOps Release Info =========="
echo "IMAGE_TAG:    ${IMAGE_TAG}"
echo "APP_VERSION:  ${APP_VERSION}"
echo "FAULT_RATE:   ${FAULT_RATE}"
echo "LATENCY_MS:   ${LATENCY_MS}"
echo "S_SENTINEL_ENV: ${S_SENTINEL_ENV}"
echo "SLO_CONFIG_FILE: ${SLO_CONFIG_FILE}"
echo "SLO_ERROR_RATE_THRESHOLD(config): ${SLO_ERROR_RATE_THRESHOLD}"
echo "SLO_P95_SECONDS_THRESHOLD(config): ${SLO_P95_SECONDS_THRESHOLD}"
echo "SLO_MIN_REQUEST_COUNT(config): ${SLO_MIN_REQUEST_COUNT}"
echo "REMOTE_IMAGE: ${REMOTE_IMAGE}"
echo "BASE_DIR:     ${BASE_DIR}"
echo "========================================="

cd "${APP_DIR}"

echo "[1/7] Prepare Go build cache..."
export HOME="${HOME:-/tmp/slo-runner-home}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-/tmp/slo-runner-cache}"
export GOCACHE="${GOCACHE:-/tmp/slo-go-build-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/slo-go-mod-cache}"

mkdir -p "$HOME" "$XDG_CACHE_HOME" "$GOCACHE" "$GOMODCACHE"

echo "[2/7] Build Go binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o demo-app main.go

echo "[2/7] Build image tar..."
python3 build-image.py "${IMAGE_TAG}" "${APP_VERSION}" "${FAULT_RATE}" "${LATENCY_MS}"

echo "[3/7] Import image into containerd..."
ctr -n k8s.io images import "${TAR_FILE}"

echo "[4/7] Tag and push image to private registry..."
ctr -n k8s.io images tag --force "${LOCAL_IMAGE}" "${REMOTE_IMAGE}"
ctr -n k8s.io images push --plain-http "${REMOTE_IMAGE}"

echo "[5/7] Compile GitOps manifests from S Sentinel configs..."

cd "${ROOT_DIR}"

COMPILED_ROOT="${COMPILED_ROOT:-/tmp/ssentinel-release-compiled}"
COMPILED_DIR="${COMPILED_ROOT}/${S_SENTINEL_ENV}"

rm -rf "${COMPILED_DIR}"

REGISTRY="${REGISTRY}" \
IMAGE_NAME="${IMAGE_NAME}" \
./scripts/compile-release-config.sh \
  --env "${S_SENTINEL_ENV}" \
  --service "${ROLLOUT_NAME}" \
  --image-tag "${IMAGE_TAG}" \
  --app-version "${APP_VERSION}" \
  --fault-rate "${FAULT_RATE}" \
  --latency-ms "${LATENCY_MS}" \
  --output-dir "${COMPILED_ROOT}"

cp "${COMPILED_DIR}/analysis.yaml" "${BASE_DIR}/analysis.yaml"
cp "${COMPILED_DIR}/rollout.yaml" "${BASE_DIR}/rollout.yaml"
cp "${COMPILED_DIR}/prometheusrule.yaml" "${BASE_DIR}/prometheusrule.yaml"

NAMESPACE="$(python3 - "$COMPILED_DIR/rendered-release-plan.json" <<'PY'
import json
import sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
print(data["release"]["namespace"])
PY
)"

echo "Compiled GitOps manifests copied into deploy/base:"
echo "- ${BASE_DIR}/analysis.yaml"
echo "- ${BASE_DIR}/rollout.yaml"
echo "- ${BASE_DIR}/prometheusrule.yaml"
echo "Compiled namespace: ${NAMESPACE}"
cd "${ROOT_DIR}"

echo "[6/7] Validate kustomize render..."
kubectl kustomize "${DEPLOY_DIR}" >/tmp/slo-rollout-rendered.yaml
grep -q "name: request-count" /tmp/slo-rollout-rendered.yaml
grep -q "version=\"${IMAGE_TAG}\"" /tmp/slo-rollout-rendered.yaml
echo "Kustomize render OK."

echo "[7/7] Commit GitOps changes..."

echo "===== Generate ChangeContext ====="
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

CHANGE_CONTEXT_OUTPUT_DIR="${CHANGE_CONTEXT_OUTPUT_DIR:-docs/release-reports}"
if [ -d /data/nfs/slo-rollout-watcher/reports ] && [ -z "${CHANGE_CONTEXT_OUTPUT_DIR_OVERRIDE:-}" ]; then
  CHANGE_CONTEXT_OUTPUT_DIR="/data/nfs/slo-rollout-watcher/reports"
fi

BASE_REF="${CHANGE_CONTEXT_BASE_REF:-HEAD}" \
OUTPUT_DIR="$CHANGE_CONTEXT_OUTPUT_DIR" \
APP_NAME="demo-app" \
NAMESPACE="slo-rollout" \
./scripts/generate-change-context.sh || {
  echo "WARN: generate-change-context.sh failed, continue release"
}

echo "ChangeContext output dir: $CHANGE_CONTEXT_OUTPUT_DIR"
# Derive real release result from Rollout phase
ROLLOUT_PHASE="$(kubectl -n "$NAMESPACE" get rollout "$ROLLOUT_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
if [ -z "$ROLLOUT_PHASE" ]; then
  RELEASE_RESULT="IN_PROGRESS"
  RELEASE_REASON="Rollout phase not available yet"
elif [ "$ROLLOUT_PHASE" = "Healthy" ]; then
  RELEASE_RESULT="PASS"
  RELEASE_REASON="Rollout is Healthy"
elif [ "$ROLLOUT_PHASE" = "Degraded" ]; then
  RELEASE_RESULT="FAIL"
  RELEASE_REASON="Rollout is Degraded"
elif [ "$ROLLOUT_PHASE" = "Paused" ]; then
  RELEASE_RESULT="IN_PROGRESS"
  RELEASE_REASON="Rollout is Paused"
else
  RELEASE_RESULT="IN_PROGRESS"
  RELEASE_REASON="Rollout phase: ${ROLLOUT_PHASE}"
fi

echo "Rollout phase: ${ROLLOUT_PHASE:-unknown}, mapped result: ${RELEASE_RESULT}"
echo "DEBUG report inputs: RELEASE_RESULT=${RELEASE_RESULT}, RELEASE_REASON=${RELEASE_REASON}"
export RELEASE_RESULT RELEASE_REASON

METRICS_ENV="$(IMAGE_TAG="$IMAGE_TAG" NAMESPACE="$NAMESPACE" ./scripts/collect-release-metrics.sh || true)"
eval "$METRICS_ENV"
echo "Observed: req_1m=${OBS_REQUEST_COUNT_1M:-unknown}, err_rate=${OBS_ERROR_RATE_PERCENT:-unknown}, p95=${OBS_P95_LATENCY_SECONDS:-unknown}"
IMAGE_TAG="$IMAGE_TAG" \
APP_VERSION="$APP_VERSION" \
NAMESPACE="$NAMESPACE" \
ROLLOUT_NAME="$ROLLOUT_NAME" \
SLO_ERROR_RATE_THRESHOLD="$SLO_ERROR_RATE_THRESHOLD" \
SLO_P95_SECONDS_THRESHOLD="$SLO_P95_SECONDS_THRESHOLD" \
SLO_MIN_REQUEST_COUNT="$SLO_MIN_REQUEST_COUNT" \
  OBS_REQUEST_COUNT_1M="${OBS_REQUEST_COUNT_1M:-unknown}" \
  OBS_ERROR_RATE_PERCENT="${OBS_ERROR_RATE_PERCENT:-unknown}" \
  OBS_P95_LATENCY_SECONDS="${OBS_P95_LATENCY_SECONDS:-unknown}" \
OUTPUT_DIR="$CHANGE_CONTEXT_OUTPUT_DIR" \
./scripts/write-release-report.sh || {
  echo "WARN: write-release-report.sh failed, continue release"
}

echo "===== Evaluate Pre-Release Change Risk ====="
LATEST_CHANGE_CONTEXT="${CHANGE_CONTEXT_OUTPUT_DIR}/change-context-latest.json"
if [ -x ./scripts/evaluate-change-risk.sh ]; then
  ./scripts/evaluate-change-risk.sh "$LATEST_CHANGE_CONTEXT" || {
    echo "WARN: evaluate-change-risk.sh failed, continue release"
  }
else
  echo "WARN: evaluate-change-risk.sh not found, skip pre-release change risk evaluation"
fi

git add "${BASE_DIR}/analysis.yaml" "${BASE_DIR}/rollout.yaml" "${BASE_DIR}/prometheusrule.yaml"

# Release reports and ChangeContext files are runtime artifacts.
# They are written to NFS or local ignored directories and should not be committed.

if git diff --cached --quiet; then
  echo "No GitOps changes to commit."
else
  git config user.name "sre-demo"
  git config user.email "sre-demo@example.com"
  git commit -m "release: ${IMAGE_TAG}"
fi

echo "Release GitOps commit finished. Push is handled by GitHub Actions or manually by the operator."
