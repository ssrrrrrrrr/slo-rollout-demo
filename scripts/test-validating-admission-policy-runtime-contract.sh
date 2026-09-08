#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-admission-policy-runtime-contract-validating-admission-policy-runtime-contract-test}"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

AI_DECISION="$TMP_DIR/ai-decision-20260101-000000.json"
REGISTRY="$TMP_DIR/policy-runtime-registry.json"
POLICY_INPUT="$TMP_DIR/policy-input-validating-admission-policy-20260101-000000.json"
POLICY_RESULT="$TMP_DIR/policy-runtime-result-validating-admission-policy-20260101-000000.json"
POLICY_DECISION="$TMP_DIR/policy-decision-validating-admission-policy-20260101-000000.json"

cat > "$AI_DECISION" <<'JSON'
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-validating-admission-policy-runtime-contract.sh",
  "releaseResult": "PASS",
  "recommendedAction": "NOOP",
  "executionMode": "advisory_only",
  "requiresHumanApproval": false,
  "service": "demo-app",
  "env": "dev",
  "sloId": "demo-app-canary-slo",
  "strategyId": "demo-app-canary-strategy",
  "agentAction": {
    "type": "NOOP",
    "allowed": true,
    "requiresApproval": false
  },
  "guardrails": {
    "autoExecute": false,
    "executionMode": "advisory_only"
  }
}
JSON

echo "===== list runtimes ====="
./scripts/policy-runtime-adapter.py list-runtimes --output "$REGISTRY"

echo "===== build validating admission policy simulator input ====="
./scripts/policy-runtime-adapter.py build-input \
  --ai-decision "$AI_DECISION" \
  --policy-file policy/release-policy.yaml \
  --runtime validating-admission-policy-sim \
  --output "$POLICY_INPUT"

echo "===== evaluate validating admission policy simulator preview runtime ====="
./scripts/policy-runtime-adapter.py evaluate \
  --runtime validating-admission-policy-sim \
  --policy-input "$POLICY_INPUT" \
  --output "$POLICY_RESULT" \
  --repo-dir "$ROOT_DIR" \
  --decision-output "$POLICY_DECISION"

echo "===== assert validating admission policy simulator runtime contract ====="
python3 - "$REGISTRY" "$POLICY_INPUT" "$POLICY_RESULT" "$POLICY_DECISION" <<'PY'
import json
import sys
from pathlib import Path

registry = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
policy_input = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
result = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
decision = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))

policy_file = Path("policy/validating-admission-policy/release-policy.yaml")
readme = Path("policy/validating-admission-policy/README.md")
assert policy_file.exists(), policy_file
assert readme.exists(), readme

policy_text = policy_file.read_text(encoding="utf-8")
readme_text = readme.read_text(encoding="utf-8")
assert "kind: ValidatingAdmissionPolicy" in policy_text, policy_text
assert "kind: ValidatingAdmissionPolicyBinding" in policy_text, policy_text
assert "ssentinel-release-policy-preview" in policy_text, policy_text
assert "ssentinel.io/runtime: validating-admission-policy-sim" in policy_text, policy_text
assert "CEL" in readme_text, readme_text

runtimes = {item["name"]: item for item in registry["runtimes"]}
vap = runtimes["validating-admission-policy-sim"]

assert vap["policyBundleRef"] == "policy/validating-admission-policy", vap
assert vap["policyFile"] == "policy/validating-admission-policy/release-policy.yaml", vap
assert vap["entrypoint"] == "ValidatingAdmissionPolicy/ssentinel-release-policy-preview", vap
assert vap["inputContract"] == "policy.input/v1alpha1", vap
assert vap["outputContract"] == "release.policy.evaluator/v1alpha1", vap
assert vap["commandPreviewTemplate"][0] == "validating-admission-policy-sim", vap
assert "${POLICY_INPUT}" in vap["commandPreviewTemplate"], vap

assert policy_input["runtime"]["requestedRuntime"] == "validating-admission-policy-sim", policy_input
assert policy_input["runtime"]["capability"]["policyBundleRef"] == "policy/validating-admission-policy", policy_input
assert policy_input["runtime"]["capability"]["policyFile"] == "policy/validating-admission-policy/release-policy.yaml", policy_input
assert policy_input["runtime"]["capability"]["entrypoint"] == "ValidatingAdmissionPolicy/ssentinel-release-policy-preview", policy_input
assert policy_input["runtime"]["capability"]["inputContract"] == "policy.input/v1alpha1", policy_input
assert policy_input["runtime"]["capability"]["outputContract"] == "release.policy.evaluator/v1alpha1", policy_input

runtime = result["runtime"]
assert runtime["name"] == "validating-admission-policy-sim", result
assert runtime["status"] == "preview_only", result
assert runtime["mode"] == "registry_preview", result
assert runtime["policyBundleRef"] == "policy/validating-admission-policy", result
assert runtime["policyFile"] == "policy/validating-admission-policy/release-policy.yaml", result
assert runtime["entrypoint"] == "ValidatingAdmissionPolicy/ssentinel-release-policy-preview", result
assert runtime["inputContract"] == "policy.input/v1alpha1", result
assert runtime["outputContract"] == "release.policy.evaluator/v1alpha1", result
assert runtime["commandPreview"][0] == "validating-admission-policy-sim", result
assert str(Path(sys.argv[2])) in runtime["commandPreview"], result

assert result["policyDecision"]["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", result
assert result["policyDecision"]["allowed"] is False, result
assert result["policyDecision"]["finalAction"] == "MANUAL_REVIEW", result
assert result["summary"]["runtimeStatus"] == "preview_only", result
assert result["safety"]["readOnly"] is True, result
assert result["safety"]["willExecute"] is False, result
assert result["safety"]["doesNotRunExternalCommands"] is True, result

assert decision["schemaVersion"] == "release.policy.evaluator/v1alpha1", decision
assert "policy_runtime_preview_only" in decision["matchedRules"], decision

print("PASS: ValidatingAdmissionPolicy simulator runtime contract preview is valid")
PY

echo "PASS: Admission Policy Runtime Contract ValidatingAdmissionPolicy simulator runtime contract test passed"
