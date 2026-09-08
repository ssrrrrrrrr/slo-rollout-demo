#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-admission-policy-runtime-contract-policy-runtime-registry-test}"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

AI_DECISION="$TMP_DIR/ai-decision-20260101-000000.json"
REGISTRY="$TMP_DIR/policy-runtime-registry.json"

cat > "$AI_DECISION" <<'JSON'
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-policy-runtime-registry.sh",
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
  },
  "evidence": {
    "service": "demo-app",
    "env": "dev"
  }
}
JSON

echo "===== list policy runtimes ====="
./scripts/policy-runtime-adapter.py list-runtimes --output "$REGISTRY"
cat "$REGISTRY"

for runtime in opa kyverno-cli validating-admission-policy-sim; do
  echo "===== preview runtime: ${runtime} ====="

  input="$TMP_DIR/policy-input-${runtime}-20260101-000000.json"
  result="$TMP_DIR/policy-runtime-result-${runtime}-20260101-000000.json"
  decision="$TMP_DIR/policy-decision-${runtime}-20260101-000000.json"

  ./scripts/policy-runtime-adapter.py build-input \
    --ai-decision "$AI_DECISION" \
    --policy-file policy/release-policy.yaml \
    --runtime "$runtime" \
    --output "$input"

  ./scripts/policy-runtime-adapter.py evaluate \
    --runtime "$runtime" \
    --policy-input "$input" \
    --output "$result" \
    --repo-dir "$ROOT_DIR" \
    --decision-output "$decision"

  python3 - "$REGISTRY" "$runtime" "$input" "$result" "$decision" <<'PY'
import json
import sys
from pathlib import Path

registry = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
runtime = sys.argv[2]
policy_input = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
result = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))
decision = json.loads(Path(sys.argv[5]).read_text(encoding="utf-8"))

names = {item["name"]: item for item in registry["runtimes"]}
for name in ["local-python", "opa", "kyverno-cli", "validating-admission-policy-sim"]:
    assert name in names, registry

assert registry["schemaVersion"] == "policy.runtime.registry/v1alpha1", registry
assert registry["defaultRuntime"] == "local-python", registry
assert names["local-python"]["canEvaluate"] is True, registry
assert names["local-python"]["previewOnly"] is False, registry

assert names[runtime]["canEvaluate"] is False, registry
assert names[runtime]["previewOnly"] is True, registry
assert names[runtime]["guardrails"]["readOnly"] is True, registry
assert names[runtime]["guardrails"]["willExecute"] is False, registry

assert policy_input["runtime"]["requestedRuntime"] == runtime, policy_input
assert policy_input["runtime"]["capability"]["name"] == runtime, policy_input
assert policy_input["runtime"]["capability"]["previewOnly"] is True, policy_input

assert result["schemaVersion"] == "policy.runtime.result/v1alpha1", result
assert result["runtime"]["name"] == runtime, result
assert result["runtime"]["status"] == "preview_only", result
assert result["runtime"]["previewOnly"] is True, result
assert result["runtime"]["guardrails"]["doesNotRunExternalCommands"] is True, result

assert result["policyDecision"]["schemaVersion"] == "release.policy.evaluator/v1alpha1", result
assert result["policyDecision"]["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", result
assert result["policyDecision"]["allowed"] is False, result
assert result["policyDecision"]["requiresHumanApproval"] is True, result
assert result["policyDecision"]["finalAction"] == "MANUAL_REVIEW", result
assert "policy_runtime_preview_only" in result["policyDecision"]["matchedRules"], result

assert result["summary"]["runtimeStatus"] == "preview_only", result
assert result["summary"]["runtimePreviewOnly"] is True, result
assert result["safety"]["readOnly"] is True, result
assert result["safety"]["willExecute"] is False, result
assert result["safety"]["doesNotRunExternalCommands"] is True, result

assert decision["schemaVersion"] == "release.policy.evaluator/v1alpha1", decision
assert decision["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", decision
assert "policy_runtime_preview_only" in decision["matchedRules"], decision
PY

done

echo "PASS: Admission Policy Runtime Contract policy runtime registry test passed"
