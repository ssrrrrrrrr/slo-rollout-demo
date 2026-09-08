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

TEST_ROOT="${TEST_ROOT:-/tmp/ssentinel-compiler-profile-rendering-renderer-refs-contract}"
PROFILE="$TEST_ROOT/demo-app-renderer-refs-test.profile.yaml"
OUT="$TEST_ROOT/out"

rm -rf "$TEST_ROOT"
mkdir -p "$TEST_ROOT" "$OUT"

cp configs/compiler-profiles/demo-app.profile.yaml "$PROFILE"

echo "===== mutate CompilerProfile renderer refs ====="
"$PYTHON_BIN" - "$PROFILE" <<'PY'
import sys
from pathlib import Path
import yaml

path = Path(sys.argv[1])
data = yaml.safe_load(path.read_text(encoding="utf-8"))

data["metadata"]["name"] = "demo-app-renderer-refs-test-profile"
data["spec"]["rendererRefs"] = {
    "rolloutTemplate": "custom-rollout-template-v2",
    "analysisTemplateRenderer": "custom-analysis-template-renderer-v2",
    "prometheusRuleRenderer": "custom-prometheus-rule-renderer-v2",
    "environmentOverlayRenderer": "custom-kustomize-overlay-renderer-v2",
}

path.write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True), encoding="utf-8")
PY

echo "===== compile with mutated renderer refs ====="
./scripts/compile-release-config.sh \
  --env dev \
  --compiler-profile "$PROFILE" \
  --image-tag "v46-renderer-refs" \
  --app-version "v46" \
  --fault-rate "0" \
  --latency-ms "0" \
  --output-dir "$OUT"

echo "===== kustomize compiled mutated renderer refs ====="
kubectl kustomize "$OUT/dev" >/tmp/ssentinel-compiler-profile-rendering-renderer-refs-contract.yaml
grep -q "kind: Rollout" /tmp/ssentinel-compiler-profile-rendering-renderer-refs-contract.yaml
grep -q "kind: AnalysisTemplate" /tmp/ssentinel-compiler-profile-rendering-renderer-refs-contract.yaml
grep -q "kind: PrometheusRule" /tmp/ssentinel-compiler-profile-rendering-renderer-refs-contract.yaml

echo "===== assert renderer refs contract ====="
"$PYTHON_BIN" - "$PROFILE" "$OUT/dev" <<'PY'
import json
import sys
from pathlib import Path
import yaml

profile_path = Path(sys.argv[1])
env_dir = Path(sys.argv[2])

profile = yaml.safe_load(profile_path.read_text(encoding="utf-8"))
analysis = yaml.safe_load((env_dir / "analysis.yaml").read_text(encoding="utf-8"))
rollout = yaml.safe_load((env_dir / "rollout.yaml").read_text(encoding="utf-8"))
prometheus_rule = yaml.safe_load((env_dir / "prometheusrule.yaml").read_text(encoding="utf-8"))
kustomization = yaml.safe_load((env_dir / "kustomization.yaml").read_text(encoding="utf-8"))
plan = json.loads((env_dir / "rendered-release-plan.json").read_text(encoding="utf-8"))

refs = profile["spec"]["rendererRefs"]

assert analysis["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == refs["analysisTemplateRenderer"], analysis
assert rollout["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == refs["rolloutTemplate"], rollout
assert prometheus_rule["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == refs["prometheusRuleRenderer"], prometheus_rule
assert kustomization["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == refs["environmentOverlayRenderer"], kustomization

assert analysis["metadata"]["annotations"]["ssentinel.io/compiler-profile"] == "demo-app-renderer-refs-test-profile", analysis
assert rollout["metadata"]["annotations"]["ssentinel.io/compiler-profile"] == "demo-app-renderer-refs-test-profile", rollout
assert prometheus_rule["metadata"]["annotations"]["ssentinel.io/compiler-profile"] == "demo-app-renderer-refs-test-profile", prometheus_rule
assert kustomization["metadata"]["annotations"]["ssentinel.io/compiler-profile"] == "demo-app-renderer-refs-test-profile", kustomization

outputs = plan["outputs"]
assert outputs["rendererRefs"]["analysisTemplate"] == refs["analysisTemplateRenderer"], outputs
assert outputs["rendererRefs"]["rollout"] == refs["rolloutTemplate"], outputs
assert outputs["rendererRefs"]["prometheusRule"] == refs["prometheusRuleRenderer"], outputs
assert outputs["rendererRefs"]["environmentOverlay"] == refs["environmentOverlayRenderer"], outputs

artifacts = {item["kind"]: item for item in outputs["artifacts"]}
assert artifacts["AnalysisTemplate"]["path"] == "analysis.yaml", artifacts
assert artifacts["AnalysisTemplate"]["rendererRef"] == refs["analysisTemplateRenderer"], artifacts
assert artifacts["Rollout"]["path"] == "rollout.yaml", artifacts
assert artifacts["Rollout"]["rendererRef"] == refs["rolloutTemplate"], artifacts
assert artifacts["PrometheusRule"]["path"] == "prometheusrule.yaml", artifacts
assert artifacts["PrometheusRule"]["rendererRef"] == refs["prometheusRuleRenderer"], artifacts
assert artifacts["Kustomization"]["path"] == "kustomization.yaml", artifacts
assert artifacts["Kustomization"]["rendererRef"] == refs["environmentOverlayRenderer"], artifacts

assert plan["compilerProfile"]["rendererRefs"] == refs, plan["compilerProfile"]
assert plan["guardrails"]["doesNotApplyKubernetes"] is True, plan["guardrails"]
assert plan["guardrails"]["doesNotCommitOrPush"] is True, plan["guardrails"]

print("PASS: RendererRefs are surfaced in rendered artifacts and RenderedReleasePlan")
PY

echo "PASS: Compiler Profile and Rendering renderer refs contract test passed"
