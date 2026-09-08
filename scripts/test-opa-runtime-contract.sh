#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-admission-policy-runtime-contract-opa-runtime-contract-test}"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

AI_DECISION="$TMP_DIR/ai-decision-20260101-000000.json"
REGISTRY="$TMP_DIR/policy-runtime-registry.json"
POLICY_INPUT="$TMP_DIR/policy-input-opa-20260101-000000.json"
POLICY_RESULT="$TMP_DIR/policy-runtime-result-opa-20260101-000000.json"
POLICY_DECISION="$TMP_DIR/policy-decision-opa-20260101-000000.json"

cat > "$AI_DECISION" <<'JSON'
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-opa-runtime-contract.sh",
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

echo "===== build opa policy input ====="
./scripts/policy-runtime-adapter.py build-input \
  --ai-decision "$AI_DECISION" \
  --policy-file policy/release-policy.yaml \
  --runtime opa \
  --output "$POLICY_INPUT"

echo "===== evaluate opa preview runtime ====="
./scripts/policy-runtime-adapter.py evaluate \
  --runtime opa \
  --policy-input "$POLICY_INPUT" \
  --output "$POLICY_RESULT" \
  --repo-dir "$ROOT_DIR" \
  --decision-output "$POLICY_DECISION"

echo "===== assert opa runtime contract ====="
python3 - "$REGISTRY" "$POLICY_INPUT" "$POLICY_RESULT" "$POLICY_DECISION" <<'PY'
import json
import sys
from pathlib import Path

registry = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
policy_input = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
result = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
decision = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))

opa_policy = Path("policy/opa/release_policy.rego")
opa_readme = Path("policy/opa/README.md")
assert opa_policy.exists(), opa_policy
assert opa_readme.exists(), opa_readme

rego = opa_policy.read_text(encoding="utf-8")
assert "package ssentinel.release" in rego, rego
assert "data.ssentinel.release.decision" in opa_readme.read_text(encoding="utf-8"), opa_readme

runtimes = {item["name"]: item for item in registry["runtimes"]}
opa = runtimes["opa"]

assert opa["policyBundleRef"] == "policy/opa", opa
assert opa["policyFile"] == "policy/opa/release_policy.rego", opa
assert opa["entrypoint"] == "data.ssentinel.release.decision", opa
assert opa["inputContract"] == "policy.input/v1alpha1", opa
assert opa["outputContract"] == "release.policy.evaluator/v1alpha1", opa
assert opa["commandPreviewTemplate"][0] == "opa", opa
assert "${POLICY_INPUT}" in opa["commandPreviewTemplate"], opa

assert policy_input["runtime"]["requestedRuntime"] == "opa", policy_input
assert policy_input["runtime"]["capability"]["policyBundleRef"] == "policy/opa", policy_input
assert policy_input["runtime"]["capability"]["entrypoint"] == "data.ssentinel.release.decision", policy_input
assert policy_input["runtime"]["capability"]["inputContract"] == "policy.input/v1alpha1", policy_input
assert policy_input["runtime"]["capability"]["outputContract"] == "release.policy.evaluator/v1alpha1", policy_input

runtime = result["runtime"]
assert runtime["name"] == "opa", result
assert runtime["status"] == "preview_only", result
assert runtime["mode"] == "registry_preview", result
assert runtime["policyBundleRef"] == "policy/opa", result
assert runtime["policyFile"] == "policy/opa/release_policy.rego", result
assert runtime["entrypoint"] == "data.ssentinel.release.decision", result
assert runtime["inputContract"] == "policy.input/v1alpha1", result
assert runtime["outputContract"] == "release.policy.evaluator/v1alpha1", result
assert runtime["commandPreview"][0] == "opa", result
assert str(Path(sys.argv[2])) in runtime["commandPreview"], result

assert result["policyDecision"]["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", result
assert result["policyDecision"]["allowed"] is False, result
assert result["policyDecision"]["finalAction"] == "MANUAL_REVIEW", result
assert result["summary"]["runtimeStatus"] == "preview_only", result
assert result["safety"]["doesNotRunExternalCommands"] is True, result

assert decision["schemaVersion"] == "release.policy.evaluator/v1alpha1", decision
assert "policy_runtime_preview_only" in decision["matchedRules"], decision

print("PASS: OPA runtime contract preview is valid")
PY

echo "PASS: Admission Policy Runtime Contract OPA runtime contract test passed"
