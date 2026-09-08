#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-policy-runtime-contract-opa-runtime-adapter-test}"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR/bin"

AI_DECISION="$TMP_DIR/ai-decision-20260101-000000.json"
POLICY_INPUT="$TMP_DIR/policy-input-opa-20260101-000000.json"
FAKE_OPA="$TMP_DIR/bin/opa"

cat > "$AI_DECISION" <<'JSON'
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-opa-runtime-adapter.sh",
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

echo "===== build opa policy input ====="
./scripts/policy-runtime-adapter.py build-input \
  --ai-decision "$AI_DECISION" \
  --policy-file policy/release-policy.yaml \
  --runtime opa \
  --output "$POLICY_INPUT"

test -s "$POLICY_INPUT"

echo "===== create fake opa binary ====="
cat > "$FAKE_OPA" <<'PY_FAKE_OPA'
#!/usr/bin/env python3
import json
import os
import sys
from pathlib import Path

mode = os.environ.get("S_SENTINEL_FAKE_OPA_MODE", "allow")

if mode == "exit_error":
    print("fake opa policy compile error", file=sys.stderr)
    sys.exit(42)

if mode == "invalid_json":
    print("{not-json")
    sys.exit(0)

if mode == "invalid_shape":
    print(json.dumps({
        "result": [
            {
                "expressions": [
                    {
                        "value": "not-a-policy-decision-object"
                    }
                ]
            }
        ]
    }))
    sys.exit(0)

input_path = None
for idx, item in enumerate(sys.argv):
    if item == "--input" and idx + 1 < len(sys.argv):
        input_path = sys.argv[idx + 1]
        break

if not input_path:
    raise SystemExit("missing --input")

policy_input = json.loads(Path(input_path).read_text(encoding="utf-8"))
summary = policy_input.get("inputSummary") or {}

profiles = {
    "allow": {
        "policyDecision": "ALLOW",
        "allowed": True,
        "finalAction": "NOOP",
        "requiresHumanApproval": False,
        "reason": "fake opa allowed PASS/NOOP release",
        "deniedReasons": [],
        "approvalRequiredReasons": [],
        "matchedRules": ["opa_pass_noop_allowed"]
    },
    "deny": {
        "policyDecision": "DENY",
        "allowed": False,
        "finalAction": "MANUAL_REVIEW",
        "requiresHumanApproval": True,
        "reason": "fake opa denied release",
        "deniedReasons": ["opa_fake_deny_reason"],
        "approvalRequiredReasons": ["opa_fake_deny_requires_review"],
        "matchedRules": ["opa_fake_deny"]
    },
    "approval": {
        "policyDecision": "REQUIRE_HUMAN_APPROVAL",
        "allowed": False,
        "finalAction": "MANUAL_REVIEW",
        "requiresHumanApproval": True,
        "reason": "fake opa requires human approval",
        "deniedReasons": [],
        "approvalRequiredReasons": ["opa_fake_requires_approval"],
        "matchedRules": ["opa_fake_requires_approval"]
    }
}

profile = profiles.get(mode, profiles["allow"])

decision = {
    "schemaVersion": "release.policy.evaluator/v1alpha1",
    "policyDecisionId": "pd-opa-" + mode + "-" + str(policy_input.get("releaseId") or "unknown"),
    "sourceDecisionFile": policy_input.get("sourceDecisionFile"),
    "releaseId": policy_input.get("releaseId"),
    "evidenceId": None,
    "service": summary.get("service"),
    "env": summary.get("env"),
    "sloId": summary.get("sloId"),
    "strategyId": summary.get("strategyId"),
    "requestedAction": summary.get("requestedAction"),
    "executionMode": "advisory_only",
    "signedReleaseGate": policy_input.get("signedReleaseGateRef") or {},
    "inputSummary": summary,
    "safety": {
        "readOnly": True,
        "willExecute": False,
        "doesNotModifyKubernetes": True,
        "doesNotModifyGitOps": True,
        "doesNotBuildOrPushImages": True
    },
    "policyRef": policy_input.get("policyRef") or {},
    **profile
}

print(json.dumps({
    "result": [
        {
            "expressions": [
                {
                    "value": decision
                }
            ]
        }
    ]
}))
PY_FAKE_OPA
chmod +x "$FAKE_OPA"

run_opa_case() {
  local mode="$1"
  local expected_status="$2"
  local expected_rule="$3"
  local expected_decision="$4"
  local expected_allowed="$5"
  local expected_final_action="$6"
  local expected_requires_approval="$7"
  local expected_exit="$8"

  local result="$TMP_DIR/policy-runtime-result-opa-${mode}.json"
  local decision="$TMP_DIR/policy-decision-opa-${mode}.json"

  echo "===== evaluate fake opa mode: ${mode} ====="

  if [[ "$mode" == "missing" ]]; then
    S_SENTINEL_POLICY_RUNTIME_EXTERNAL_COMMANDS=1 \
    S_SENTINEL_OPA_BIN="$TMP_DIR/bin/missing-opa" \
    ./scripts/policy-runtime-adapter.py evaluate \
      --runtime opa \
      --policy-input "$POLICY_INPUT" \
      --output "$result" \
      --repo-dir "$ROOT_DIR" \
      --decision-output "$decision"
  else
    S_SENTINEL_POLICY_RUNTIME_EXTERNAL_COMMANDS=1 \
    S_SENTINEL_OPA_BIN="$FAKE_OPA" \
    S_SENTINEL_FAKE_OPA_MODE="$mode" \
    ./scripts/policy-runtime-adapter.py evaluate \
      --runtime opa \
      --policy-input "$POLICY_INPUT" \
      --output "$result" \
      --repo-dir "$ROOT_DIR" \
      --decision-output "$decision"
  fi

  test -s "$result"
  test -s "$decision"

  python3 - "$result" "$decision" "$expected_status" "$expected_rule" "$expected_decision" "$expected_allowed" "$expected_final_action" "$expected_requires_approval" "$expected_exit" "$mode" <<'PY'
import json
import sys
from pathlib import Path

result = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
decision = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))

expected_status = sys.argv[3]
expected_rule = sys.argv[4]
expected_decision = sys.argv[5]
expected_allowed = sys.argv[6].lower() == "true"
expected_final_action = sys.argv[7]
expected_requires_approval = sys.argv[8].lower() == "true"
expected_exit = None if sys.argv[9] == "null" else int(sys.argv[9])
mode = sys.argv[10]

assert result["schemaVersion"] == "policy.runtime.result/v1alpha1", result
assert result["runtime"]["name"] == "opa", result
assert result["runtime"]["status"] == expected_status, result
assert result["runtime"]["mode"] == "external_command", result
assert result["runtime"]["externalCommandEnabled"] is True, result
assert result["runtime"]["exitCode"] == expected_exit, result

policy = result["policyDecision"]
assert policy["schemaVersion"] == "release.policy.evaluator/v1alpha1", result
assert policy["policyDecision"] == expected_decision, result
assert policy["allowed"] is expected_allowed, result
assert policy["finalAction"] == expected_final_action, result
assert policy["requiresHumanApproval"] is expected_requires_approval, result
assert expected_rule in policy["matchedRules"], result

assert result["summary"]["runtimeStatus"] == expected_status, result
assert result["summary"]["requiresHumanApproval"] is expected_requires_approval, result
assert result["safety"]["readOnly"] is True, result
assert result["safety"]["willExecute"] is False, result
assert result["safety"]["doesNotModifyKubernetes"] is True, result
assert result["safety"]["doesNotModifyGitOps"] is True, result

if mode == "missing":
    assert result["runtime"]["binaryAvailable"] is False, result
    assert result["runtime"]["externalCommandExecuted"] is False, result
    assert result["summary"]["runtimeExternalCommandExecuted"] is False, result
    assert result["safety"]["externalCommandExecuted"] is False, result
    assert result["safety"]["doesNotRunExternalCommands"] is True, result
else:
    assert result["runtime"]["binaryAvailable"] is True, result
    assert result["runtime"]["externalCommandExecuted"] is True, result
    assert result["summary"]["runtimeExternalCommandExecuted"] is True, result
    assert result["safety"]["externalCommandExecuted"] is True, result
    assert result["safety"]["doesNotRunExternalCommands"] is False, result

assert decision["schemaVersion"] == "release.policy.evaluator/v1alpha1", decision
assert decision["policyDecision"] == expected_decision, decision
assert expected_rule in decision["matchedRules"], decision
PY
}

run_opa_case missing runtime_unavailable policy_runtime_unavailable REQUIRE_HUMAN_APPROVAL false MANUAL_REVIEW true null
run_opa_case exit_error runtime_error policy_runtime_error REQUIRE_HUMAN_APPROVAL false MANUAL_REVIEW true 42
run_opa_case invalid_json runtime_error policy_runtime_invalid_output REQUIRE_HUMAN_APPROVAL false MANUAL_REVIEW true 0
run_opa_case invalid_shape runtime_error policy_runtime_invalid_output REQUIRE_HUMAN_APPROVAL false MANUAL_REVIEW true 0
run_opa_case allow evaluated opa_pass_noop_allowed ALLOW true NOOP false 0
run_opa_case deny evaluated opa_fake_deny DENY false MANUAL_REVIEW true 0
run_opa_case approval evaluated opa_fake_requires_approval REQUIRE_HUMAN_APPROVAL false MANUAL_REVIEW true 0

echo "PASS: Policy Runtime Contract OPA runtime adapter guarded execution and failure contract test passed"
