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

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-evidence-store-test}"
FIXTURE_DIR="$TMP_DIR/reports"
DB_FILE="$TMP_DIR/evidence-store.db"
QUERY_JSON="$TMP_DIR/query-release.json"
LIST_JSON="$TMP_DIR/list-releases.json"
OBJECT_JSON="$TMP_DIR/get-object.json"
RELEASE_ID="20260101-000000"

rm -rf "$TMP_DIR"
mkdir -p "$FIXTURE_DIR"

cat > "$FIXTURE_DIR/release-evidence-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:00Z",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "policyDecisionId": "pd-$RELEASE_ID",
  "executionMode": "advisory_only",
  "requiresHumanApproval": true,
  "safeToRetry": false,
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "summary": {
    "riskLevel": "critical",
    "riskScore": 100,
    "failedMetrics": ["error-rate", "p95-latency"]
  },
  "artifacts": {
    "releaseContext": "release-context-$RELEASE_ID.json",
    "agentRun": "agent-run-$RELEASE_ID.json",
    "agentTrace": "agent-trace-$RELEASE_ID.json",
    "planRun": "plan-run-$RELEASE_ID.json",
    "executionRequest": "execution-request-$RELEASE_ID.json",
    "supplyChainDecision": "supply-chain-decision-$RELEASE_ID.json"
  },
  "agentRunId": "ar-$RELEASE_ID",
  "agentTraceId": "at-$RELEASE_ID",
  "planRunId": "pr-$RELEASE_ID",
  "executionRequestId": "er-$RELEASE_ID",
  "supplyChainDecisionId": "sc-$RELEASE_ID"
}
JSON

cat > "$FIXTURE_DIR/evidence-record-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "evidence.record/v1alpha1",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:01Z",
  "evidenceId": "ev-$RELEASE_ID-demo-app-dev",
  "releaseId": "$RELEASE_ID",
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "sourceEvidence": "release-evidence-$RELEASE_ID.json",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "requiresHumanApproval": true,
  "summary": {
    "riskLevel": "critical",
    "riskScore": 100
  },
  "artifacts": {
    "releaseEvidence": {
      "kind": "releaseEvidence",
      "path": "release-evidence-$RELEASE_ID.json",
      "exists": true
    }
  },
  "links": {
    "releaseEvidence": "release-evidence-$RELEASE_ID.json"
  }
}
JSON

cat > "$FIXTURE_DIR/agent-run-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "agent.run/v1alpha1",
  "agentRunId": "ar-$RELEASE_ID",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:02Z",
  "mode": "read_only",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "releaseResult": "FAIL_BY_MULTIPLE_SLO"
  },
  "recommendation": {
    "recommendedAction": "STOP_PROMOTION",
    "priority": "critical",
    "willExecute": false
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$FIXTURE_DIR/agent-trace-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "agent.trace/v1alpha1",
  "agentTraceId": "at-$RELEASE_ID",
  "traceId": "trace-$RELEASE_ID",
  "releaseId": "$RELEASE_ID",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:03Z",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "releaseResult": "FAIL_BY_MULTIPLE_SLO"
  },
  "correlation": {
    "releaseId": "$RELEASE_ID",
    "agentRunId": "ar-$RELEASE_ID",
    "policyDecisionId": "pd-$RELEASE_ID",
    "policyRuntimeResultId": "prr-$RELEASE_ID",
    "signedReleaseGateId": "srg-$RELEASE_ID"
  },
  "agentRun": {
    "agentRunId": "ar-$RELEASE_ID",
    "status": "COMPLETED"
  },
  "policyTrace": {
    "policyDecision": "REQUIRE_HUMAN_APPROVAL",
    "finalAction": "STOP_PROMOTION",
    "requiresHumanApproval": true,
    "matchedRules": [
      "signed_release_gate_requires_human_approval"
    ]
  },
  "toolCallTraces": [
    {
      "name": "ai_release_advisor",
      "tool": "ai-release-advisor.sh",
      "status": "AVAILABLE",
      "readOnly": true,
      "willExecute": false
    }
  ],
  "evidenceTrace": {
    "releaseEvidence": "release-evidence-$RELEASE_ID.json"
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$FIXTURE_DIR/plan-run-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "agent.plan.run/v1alpha1",
  "planRunId": "pr-$RELEASE_ID",
  "sourceAgentRunId": "ar-$RELEASE_ID",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:03Z",
  "mode": "read_only_planning",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "releaseResult": "FAIL_BY_MULTIPLE_SLO",
    "policyDecision": "REQUIRE_HUMAN_APPROVAL",
    "recommendedAction": "STOP_PROMOTION"
  },
  "plan": {
    "planType": "rag_assisted_failure_investigation",
    "priority": "critical",
    "willExecute": false
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$FIXTURE_DIR/execution-request-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "execution.request/v1alpha1",
  "executionRequestId": "er-$RELEASE_ID",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:04Z",
  "mode": "request_only",
  "sourcePlanRunId": "pr-$RELEASE_ID",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "releaseResult": "FAIL_BY_MULTIPLE_SLO",
    "policyDecision": "REQUIRE_HUMAN_APPROVAL"
  },
  "request": {
    "requestedAction": "STOP_PROMOTION",
    "requestStatus": "PENDING_APPROVAL",
    "willExecute": false
  },
  "policyBinding": {
    "policyDecision": "REQUIRE_HUMAN_APPROVAL",
    "requiresHumanApproval": true,
    "willExecute": false
  },
  "guardrails": {
    "requestOnly": true,
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$FIXTURE_DIR/supply-chain-decision-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "supply.chain.decision/v1alpha1",
  "supplyChainDecisionId": "sc-$RELEASE_ID",
  "generatedBy": "test-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:05Z",
  "mode": "read_only_supply_chain_check",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "releaseResult": "FAIL_BY_MULTIPLE_SLO"
  },
  "image": {
    "image": "registry.local:5000/sre/demo-app:v-test",
    "imageTag": "v-test",
    "imageDigest": null
  },
  "decision": {
    "decision": "REQUIRE_HUMAN_APPROVAL",
    "requiresHumanApproval": true,
    "allowed": true
  },
  "verification": {
    "schemaVersion": "signed.release.gate.verification/v1alpha1",
    "mode": "external_command",
    "tool": "cosign",
    "toolBinary": "/tmp/ssentinel-missing-cosign",
    "toolAvailable": false,
    "command": null,
    "commandPreview": ["/tmp/ssentinel-missing-cosign", "verify", "registry.local:5000/sre/demo-app:v-test"],
    "exitCode": null,
    "checkedAt": "2026-01-01T00:00:05Z",
    "subject": {
      "image": "registry.local:5000/sre/demo-app:v-test",
      "imageDigest": null
    },
    "results": {
      "imageDigestPresent": false,
      "usesDigestReference": false,
      "signatureVerified": false,
      "sbomPresent": false,
      "provenancePresent": false,
      "slsaLevelPresent": false,
      "slsaLevel": null
    },
    "guardrails": {
      "readOnly": true,
      "willExecute": false,
      "canRunExternalVerification": false,
      "doesNotRunExternalCommands": true,
      "doesNotVerifyExternalServices": true
    }
  },
  "risk": {
    "riskLevel": "high",
    "riskScore": 70
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

echo "===== init db ====="
"$PYTHON_BIN" ./scripts/evidence-store.py init-db --db "$DB_FILE"

echo
echo "===== import fixture dir ====="
"$PYTHON_BIN" ./scripts/evidence-store.py import-dir --db "$DB_FILE" --report-dir "$FIXTURE_DIR"

echo
echo "===== query release ====="
"$PYTHON_BIN" ./scripts/evidence-store.py query-release --db "$DB_FILE" --release-id "$RELEASE_ID" > "$QUERY_JSON"
cat "$QUERY_JSON"

echo
echo "===== assert query result ====="
"$PYTHON_BIN" - "$QUERY_JSON" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
release = data["release"]
objects = data["objects"]
kinds = {item["object_type"] for item in objects}
expected = {
    "releaseEvidence",
    "evidenceRecord",
    "agentRun",
    "agentTrace",
    "planRun",
    "executionRequest",
    "supplyChainDecision",
}

assert release["release_id"] == "20260101-000000", release
assert release["service"] == "demo-app", release
assert release["env"] == "dev", release
assert release["release_result"] == "FAIL_BY_MULTIPLE_SLO", release
assert release["policy_decision"] == "REQUIRE_HUMAN_APPROVAL", release
assert release["final_action"] == "STOP_PROMOTION", release
assert expected.issubset(kinds), kinds
assert data["objectCount"] == 7, data["objectCount"]
assert data["artifactCount"] >= 1, data["artifactCount"]

ids = {item["object_type"]: item["object_id"] for item in objects}
assert ids["evidenceRecord"].startswith("ev-")
assert ids["agentRun"].startswith("ar-")
assert ids["agentTrace"].startswith("at-")
assert ids["planRun"].startswith("pr-")
assert ids["executionRequest"].startswith("er-")
assert ids["supplyChainDecision"].startswith("sc-")

print("PASS: EvidenceStore query result is valid")
PY

echo
echo "===== list releases ====="
"$PYTHON_BIN" ./scripts/evidence-store.py list-releases --db "$DB_FILE" --limit 10 > "$LIST_JSON"
cat "$LIST_JSON"

echo
echo "===== get object ====="
"$PYTHON_BIN" ./scripts/evidence-store.py get-object \
  --db "$DB_FILE" \
  --object-type supplyChainDecision \
  --object-id "sc-$RELEASE_ID" \
  --release-id "$RELEASE_ID" > "$OBJECT_JSON"
cat "$OBJECT_JSON"

echo
echo "===== assert list and object query ====="
"$PYTHON_BIN" - "$LIST_JSON" "$OBJECT_JSON" <<'PY_ASSERT_LIST_OBJECT'
import json
import sys
from pathlib import Path

release_list = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
obj = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))

assert release_list["schemaVersion"] == "evidence.store.releaseList/v1alpha1"
assert release_list["count"] >= 1
item = release_list["items"][0]
assert item["release_id"] == "20260101-000000", item
assert item["object_count"] == 7, item
assert "supplyChainDecision" in item["object_types"], item["object_types"]

assert obj["schemaVersion"] == "evidence.store.object/v1alpha1"
assert obj["release"]["release_id"] == "20260101-000000", obj["release"]
assert obj["object"]["object_type"] == "supplyChainDecision", obj["object"]
assert obj["object"]["object_id"] == "sc-20260101-000000", obj["object"]
summary = obj["object"]["summary"]
assert summary["decision"] == "REQUIRE_HUMAN_APPROVAL", summary
assert summary["verificationMode"] == "external_command", summary
assert summary["verificationToolAvailable"] is False, summary
assert summary["signatureVerified"] is False, summary
assert summary["sbomPresent"] is False, summary
assert summary["provenancePresent"] is False, summary
assert summary["canRunExternalVerification"] is False, summary
assert summary["verification"]["doesNotRunExternalCommands"] is True, summary

print("PASS: EvidenceStore list and object query are valid")
PY_ASSERT_LIST_OBJECT

echo
echo "PASS: evidence store test passed"
