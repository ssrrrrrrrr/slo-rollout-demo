#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BAD_PROFILE="configs/compiler-profiles/zz-invalid-compiler-profile-rendering.profile.yaml"
cleanup() {
  rm -f "$BAD_PROFILE"
}
trap cleanup EXIT

echo "===== validate current compiler profiles ====="
scripts/validate-compiler-profile.sh

echo "===== create invalid compiler profile fixture ====="
cat > "$BAD_PROFILE" <<'YAML'
apiVersion: compiler.ssentinel.io/v1alpha1
kind: CompilerProfile
metadata:
  name: invalid-compiler-profile-rendering-profile
  service: demo-app
spec:
  serviceConfig:
    serviceName: demo-app
    containerName: demo-app
    servicePortName: http
    containerPort: 0
    health:
      readinessPath: readyz
      livenessPath: /livez
  runtimeProfile:
    runtimeType: container
    replicas: 0
    revisionHistoryLimit: -1
    imagePullPolicy: Sometimes
  metricBinding:
    provider: prometheus
    bindingSource: SLOConfig.spec.observability.prometheus
    prometheus:
      requestCounter: ""
      latencyHistogram: ""
      labels:
        namespace: ""
        version: version
        status: status
      errorStatusRegex: ""
    supportedObjectiveTypes:
      - request_count
  rendererRefs:
    rolloutTemplate: ""
    analysisTemplateRenderer: ""
    prometheusRuleRenderer: ""
    environmentOverlayRenderer: ""
  guardrails:
    readOnly: false
    willExecute: true
    doesNotApplyKubernetes: false
YAML

echo "===== assert invalid compiler profile is rejected ====="
set +e
OUTPUT="$(scripts/validate-compiler-profile.sh 2>&1)"
STATUS=$?
set -e

echo "$OUTPUT"

if [ "$STATUS" -eq 0 ]; then
  echo "FAIL: invalid CompilerProfile unexpectedly passed validation" >&2
  exit 1
fi

grep -q "spec.serviceConfig.containerPort must be a positive integer" <<<"$OUTPUT"
grep -q "spec.runtimeProfile.replicas must be >= 1" <<<"$OUTPUT"
grep -q "spec.metricBinding.bindingSource must point to CompilerProfile.spec.metricBinding.prometheus" <<<"$OUTPUT"
grep -q "spec.guardrails.doesNotApplyKubernetes must be true" <<<"$OUTPUT"

cleanup

echo "===== validate current compiler profiles after cleanup ====="
scripts/validate-compiler-profile.sh

echo "PASS: Compiler Profile and Rendering CompilerProfile validation gate test passed"
