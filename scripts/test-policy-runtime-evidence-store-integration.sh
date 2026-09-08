#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-policy-runtime-contract-policy-runtime-evidence-store-test}"
rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR/bin"

RID="20260101-000000"
AI_DECISION="$TMP_DIR/ai-decision-$RID.json"
POLICY_INPUT="$TMP_DIR/policy-input-$RID.json"
POLICY_RESULT="$TMP_DIR/policy-runtime-result-$RID.json"
POLICY_DECISION="$TMP_DIR/policy-decision-$RID.json"
DB_FILE="$TMP_DIR/evidence-store.db"
FAKE_OPA="$TMP_DIR/bin/opa"

cat > "$AI_DECISION" <<JSON
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-policy-runtime-evidence-store-integration.sh",
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

cat > "$FAKE_OPA" <<'PY_FAKE_OPA'
#!/usr/bin/env python3
import json
import sys
from pathlib import Path

input_path = None
for idx, item in enumerate(sys.argv):
    if item == "--input" and idx + 1 < len(sys.argv):
        input_path = sys.argv[idx + 1]
        break

if not input_path:
    raise SystemExit("missing --input")

policy_input = json.loads(Path(input_path).read_text(encoding="utf-8"))
summary = policy_input.get("inputSummary") or {}
release_id = policy_input.get("releaseId") or "unknown"

decision = {
    "schemaVersion": "release.policy.evaluator/v1alpha1",
    "policyDecisionId": "pd-" + release_id,
    "sourceDecisionFile": policy_input.get("sourceDecisionFile"),
    "releaseId": release_id,
    "evidenceId": None,
    "service": summary.get("service"),
    "env": summary.get("env"),
    "sloId": summary.get("sloId"),
    "strategyId": summary.get("strategyId"),
    "policyDecision": "ALLOW",
    "requestedAction": summary.get("requestedAction"),
    "allowed": True,
    "finalAction": "NOOP",
    "executionMode": "advisory_only",
    "requiresHumanApproval": False,
    "reason": "fake opa evidence store integration allowed PASS/NOOP release",
    "deniedReasons": [],
    "approvalRequiredReasons": [],
    "matchedRules": ["opa_evidence_store_allowed"],
    "signedReleaseGate": policy_input.get("signedReleaseGateRef") or {},
    "inputSummary": summary,
    "safety": {
        "readOnly": True,
        "willExecute": False,
        "doesNotModifyKubernetes": True,
        "doesNotModifyGitOps": True,
        "doesNotBuildOrPushImages": True
    },
    "policyRef": policy_input.get("policyRef") or {}
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

echo "===== build opa policy input ====="
./scripts/policy-runtime-adapter.py build-input \
  --ai-decision "$AI_DECISION" \
  --policy-file policy/release-policy.yaml \
  --runtime opa \
  --output "$POLICY_INPUT"

echo "===== evaluate fake opa runtime ====="
S_SENTINEL_POLICY_RUNTIME_EXTERNAL_COMMANDS=1 \
S_SENTINEL_OPA_BIN="$FAKE_OPA" \
./scripts/policy-runtime-adapter.py evaluate \
  --runtime opa \
  --policy-input "$POLICY_INPUT" \
  --output "$POLICY_RESULT" \
  --repo-dir "$ROOT_DIR" \
  --decision-output "$POLICY_DECISION"

echo "===== import policy runtime objects ====="
./scripts/evidence-store.py init-db --db "$DB_FILE" >/dev/null
./scripts/evidence-store.py import-dir \
  --db "$DB_FILE" \
  --report-dir "$TMP_DIR" \
  > "$TMP_DIR/import-result.json"

./scripts/evidence-store.py get-object \
  --db "$DB_FILE" \
  --object-type policyInput \
  --object-id "pi-$RID" \
  --release-id "$RID" \
  > "$TMP_DIR/policy-input-object.json"

./scripts/evidence-store.py get-object \
  --db "$DB_FILE" \
  --object-type policyRuntimeResult \
  --object-id "prr-$RID" \
  --release-id "$RID" \
  > "$TMP_DIR/policy-runtime-result-object.json"

./scripts/evidence-store.py get-object \
  --db "$DB_FILE" \
  --object-type policyDecision \
  --object-id "pd-$RID" \
  --release-id "$RID" \
  > "$TMP_DIR/policy-decision-object.json"

./scripts/evidence-store.py search-objects \
  --db "$DB_FILE" \
  --query opa_evidence_store_allowed \
  --limit 10 \
  > "$TMP_DIR/search-result.json"

python3 - "$TMP_DIR" "$RID" <<'PY'
import json
import sys
from pathlib import Path

tmp = Path(sys.argv[1])
rid = sys.argv[2]

def load(name):
    return json.loads((tmp / name).read_text(encoding="utf-8"))

import_result = load("import-result.json")
assert import_result["schemaVersion"] == "evidence.store.import/v1alpha1", import_result
assert import_result["byType"]["policyInput"] == 1, import_result
assert import_result["byType"]["policyRuntimeResult"] == 1, import_result
assert import_result["byType"]["policyDecision"] == 1, import_result

policy_input = load("policy-input-object.json")
policy_runtime = load("policy-runtime-result-object.json")
policy_decision = load("policy-decision-object.json")
search_result = load("search-result.json")

assert policy_input["object"]["object_type"] == "policyInput", policy_input
assert policy_input["object"]["object_id"] == "pi-" + rid, policy_input

assert policy_runtime["object"]["object_type"] == "policyRuntimeResult", policy_runtime
assert policy_runtime["object"]["object_id"] == "prr-" + rid, policy_runtime
assert policy_runtime["object"]["summary"]["policyDecision"] == "ALLOW", policy_runtime
assert policy_runtime["object"]["summary"]["finalAction"] == "NOOP", policy_runtime

assert policy_decision["object"]["object_type"] == "policyDecision", policy_decision
assert policy_decision["object"]["object_id"] == "pd-" + rid, policy_decision
assert policy_decision["object"]["summary"]["policyDecision"] == "ALLOW", policy_decision
assert "opa_evidence_store_allowed" in policy_decision["object"]["summary"]["matchedRules"], policy_decision

assert search_result["schemaVersion"] == "evidence.store.search/v1alpha1", search_result
assert search_result["count"] >= 1, search_result

print("PASS: OPA PolicyRuntime objects are imported and queryable in EvidenceStore")
PY

echo "PASS: Policy Runtime Contract PolicyRuntime EvidenceStore integration test passed"
