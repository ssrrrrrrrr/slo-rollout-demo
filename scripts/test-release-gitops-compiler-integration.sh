#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [ -z "${PYTHON_BIN:-}" ]; then
  if command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="python3"
  elif command -v python >/dev/null 2>&1; then
    PYTHON_BIN="python"
  else
    echo "ERROR: python runtime not found. Set PYTHON_BIN=/path/to/python." >&2
    exit 1
  fi
fi

export PYTHON_BIN

SCRIPT="demo-app/release-gitops.sh"
TEST_OUT="${TEST_OUT:-/tmp/ssentinel-release-gitops-compiler-integration}"

echo "===== release-gitops compiler integration test ====="

echo
echo "===== syntax check ====="
bash -n "$SCRIPT"

echo
echo "===== assert compiler integration exists ====="
grep -q 'S_SENTINEL_ENV="${S_SENTINEL_ENV:-dev}"' "$SCRIPT"
grep -q './scripts/compile-release-config.sh' "$SCRIPT"
grep -q 'cp "${COMPILED_DIR}/analysis.yaml" "${BASE_DIR}/analysis.yaml"' "$SCRIPT"
grep -q 'cp "${COMPILED_DIR}/rollout.yaml" "${BASE_DIR}/rollout.yaml"' "$SCRIPT"
grep -q 'cp "${COMPILED_DIR}/prometheusrule.yaml" "${BASE_DIR}/prometheusrule.yaml"' "$SCRIPT"
grep -q 'git add "${BASE_DIR}/analysis.yaml" "${BASE_DIR}/rollout.yaml" "${BASE_DIR}/prometheusrule.yaml"' "$SCRIPT"

echo
echo "===== assert old heredoc renderer removed ====="
if grep -qE 'EOF_ANALYSIS|EOF_ROLLOUT|cat > "\$\{BASE_DIR\}/analysis.yaml"|cat > "\$\{BASE_DIR\}/rollout.yaml"' "$SCRIPT"; then
  echo "FAIL: old heredoc renderer is still present" >&2
  exit 1
fi

echo
echo "===== compile dry output for release script inputs ====="
rm -rf "$TEST_OUT"

REGISTRY="registry.local:5000" \
./scripts/compile-release-config.sh \
  --env dev \
  --service demo-app \
  --image-tag v36-release-integration \
  --app-version v36 \
  --fault-rate 0 \
  --latency-ms 0 \
  --output-dir "$TEST_OUT"

echo
echo "===== verify compiled output shape ====="
test -f "$TEST_OUT/dev/analysis.yaml"
test -f "$TEST_OUT/dev/rollout.yaml"
test -f "$TEST_OUT/dev/prometheusrule.yaml"
test -f "$TEST_OUT/dev/rendered-release-plan.json"

grep -q 'successCondition: result\[0\] <= 5' "$TEST_OUT/dev/analysis.yaml"
grep -q 'successCondition: isNaN(result\[0\]) || result\[0\] <= 0.5' "$TEST_OUT/dev/analysis.yaml"
grep -q 'image: registry.local:5000/sre/demo-app:v36-release-integration' "$TEST_OUT/dev/rollout.yaml"
grep -q 'alert: DemoAppCanaryP95LatencySLOViolation' "$TEST_OUT/dev/prometheusrule.yaml"
grep -q '> 0.5' "$TEST_OUT/dev/prometheusrule.yaml"

echo
echo "===== kustomize compiled output ====="
kubectl kustomize "$TEST_OUT/dev" >/tmp/ssentinel-release-gitops-compiler-integration.yaml
grep -q 'kind: Rollout' /tmp/ssentinel-release-gitops-compiler-integration.yaml
grep -q 'kind: AnalysisTemplate' /tmp/ssentinel-release-gitops-compiler-integration.yaml
grep -q 'kind: PrometheusRule' /tmp/ssentinel-release-gitops-compiler-integration.yaml

echo
echo "PASS: release-gitops.sh uses Config Compiler for GitOps manifest rendering"
