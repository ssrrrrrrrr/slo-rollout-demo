#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-admission-policy-runtime-contract-kyverno-runtime-contract-test}"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

AI_DECISION="$TMP_DIR/ai-decision-20260101-000000.json"
REGISTRY="$TMP_DIR/policy-runtime-registry.json"
POLICY_INPUT="$TMP_DIR/policy-input-kyverno-20260101-000000.json"
POLICY_RESULT="$TMP_DIR/policy-runtime-result-kyverno-20260101-000000.json"
POLICY_DECISION="$TMP_DIR/policy-decision-kyverno-20260101-000000.json"

cat > "$AI_DECISION" <<'JSON'
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-kyverno-runtime-contract.sh",
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

echo "===== build kyverno policy input ====="
./scripts/policy-runtime-adapter.py build-input \
  --ai-decision "$AI_DECISION" \
  --policy-file policy/release-policy.yaml \
  --runtime kyverno-cli \
  --output "$POLICY_INPUT"

echo "===== evaluate kyverno preview runtime ====="
./scripts/policy-runtime-adapter.py evaluate \
  --runtime kyverno-cli \
  --policy-input "$POLICY_INPUT" \
  --output "$POLICY_RESULT" \
  --repo-dir "$ROOT_DIR" \
  --decision-output "$POLICY_DECISION"

echo "===== assert kyverno runtime contract ====="
python3 - "$REGISTRY" "$POLICY_INPUT" "$POLICY_RESULT" "$POLICY_DECISION" <<'PY'
import json
import sys
from pathlib import Path

registry = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
policy_input = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
result = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
decision = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))

policy_file = Path("policy/kyverno/release-policy.yaml")
readme = Path("policy/kyverno/README.md")
assert policy_file.exists(), policy_file
assert readme.exists(), readme

policy_text = policy_file.read_text(encoding="utf-8")
readme_text = readme.read_text(encoding="utf-8")
assert "kind: ClusterPolicy" in policy_text, policy_text
assert "ssentinel-release-policy-preview" in policy_text, policy_text
assert "ssentinel.io/runtime: kyverno-cli" in policy_text, policy_text
assert "kyverno apply" in readme_text, readme_text

runtimes = {item["name"]: item for item in registry["runtimes"]}
kyverno = runtimes["kyverno-cli"]

assert kyverno["policyBundleRef"] == "policy/kyverno", kyverno
assert kyverno["policyFile"] == "policy/kyverno/release-policy.yaml", kyverno
assert kyverno["entrypoint"] == "ClusterPolicy/ssentinel-release-policy-preview", kyverno
assert kyverno["inputContract"] == "policy.input/v1alpha1", kyverno
assert kyverno["outputContract"] == "release.policy.evaluator/v1alpha1", kyverno
assert kyverno["commandPreviewTemplate"][0] == "kyverno", kyverno
assert "${POLICY_INPUT}" in kyverno["commandPreviewTemplate"], kyverno

assert policy_input["runtime"]["requestedRuntime"] == "kyverno-cli", policy_input
assert policy_input["runtime"]["capability"]["policyBundleRef"] == "policy/kyverno", policy_input
assert policy_input["runtime"]["capability"]["policyFile"] == "policy/kyverno/release-policy.yaml", policy_input
assert policy_input["runtime"]["capability"]["entrypoint"] == "ClusterPolicy/ssentinel-release-policy-preview", policy_input
assert policy_input["runtime"]["capability"]["inputContract"] == "policy.input/v1alpha1", policy_input
assert policy_input["runtime"]["capability"]["outputContract"] == "release.policy.evaluator/v1alpha1", policy_input

runtime = result["runtime"]
assert runtime["name"] == "kyverno-cli", result
assert runtime["status"] == "preview_only", result
assert runtime["mode"] == "registry_preview", result
assert runtime["policyBundleRef"] == "policy/kyverno", result
assert runtime["policyFile"] == "policy/kyverno/release-policy.yaml", result
assert runtime["entrypoint"] == "ClusterPolicy/ssentinel-release-policy-preview", result
assert runtime["inputContract"] == "policy.input/v1alpha1", result
assert runtime["outputContract"] == "release.policy.evaluator/v1alpha1", result
assert runtime["commandPreview"][0] == "kyverno", result
assert str(Path(sys.argv[2])) in runtime["commandPreview"], result

assert result["policyDecision"]["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", result
assert result["policyDecision"]["allowed"] is False, result
assert result["policyDecision"]["finalAction"] == "MANUAL_REVIEW", result
assert result["summary"]["runtimeStatus"] == "preview_only", result
assert result["safety"]["doesNotRunExternalCommands"] is True, result

assert decision["schemaVersion"] == "release.policy.evaluator/v1alpha1", decision
assert "policy_runtime_preview_only" in decision["matchedRules"], decision

print("PASS: Kyverno runtime contract preview is valid")
PY

echo "PASS: Admission Policy Runtime Contract Kyverno runtime contract test passed"
