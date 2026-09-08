#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-release-evidence-traceid}"
REPORT_DIR="$TMP_DIR/reports"
RELEASE_ID="20260101-000000"

rm -rf "$TMP_DIR"
mkdir -p "$REPORT_DIR"

echo "===== syntax checks ====="
bash -n scripts/build-release-evidence.sh
bash -n scripts/test-release-evidence-traceid.sh
python3 - <<'PY'
import json
from pathlib import Path
json.loads(Path("schemas/release-evidence.schema.json").read_text(encoding="utf-8"))
print("PASS: release evidence schema parses")
PY

cat > "$REPORT_DIR/release-context-$RELEASE_ID.json" <<JSON
{
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev"
}
JSON

cat > "$REPORT_DIR/ai-decision-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "releaseId": "$RELEASE_ID",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "recommendedAction": "STOP_PROMOTION",
  "requiresHumanApproval": true,
  "evidence": {
    "releaseId": "$RELEASE_ID"
  },
  "sources": {
    "releaseContext": "$REPORT_DIR/release-context-$RELEASE_ID.json"
  }
}
JSON

cat > "$REPORT_DIR/policy-decision-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.policy.evaluator/v1alpha1",
  "policyDecisionId": "pd-$RELEASE_ID",
  "releaseId": "$RELEASE_ID",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "allowed": true,
  "requiresHumanApproval": true
}
JSON

cat > "$REPORT_DIR/agent-trace-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "agent.trace/v1alpha1",
  "agentTraceId": "at-$RELEASE_ID",
  "traceId": "trace-$RELEASE_ID",
  "releaseId": "$RELEASE_ID"
}
JSON

cat > "$REPORT_DIR/otel-span-bundle-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "otel.span.bundle/v1alpha1",
  "kind": "OtelSpanBundle",
  "traceId": "trace-$RELEASE_ID",
  "rootSpanId": "span-agent-root-$RELEASE_ID",
  "releaseId": "$RELEASE_ID",
  "source": {
    "kind": "AgentTrace",
    "schemaVersion": "agent.trace/v1alpha1",
    "agentTraceId": "at-$RELEASE_ID"
  },
  "resource": {},
  "spans": [],
  "summary": {
    "spanCount": 0,
    "hasRootSpan": false,
    "sourceAgentTraceId": "at-$RELEASE_ID",
    "releaseId": "$RELEASE_ID",
    "spanNames": []
  },
  "guardrails": {
    "localFileOnly": true,
    "doesNotSendExternalTelemetry": true,
    "doesNotCallExternalCollector": true,
    "doesNotModifyCluster": true,
    "doesNotModifyGitOps": true,
    "doesNotCommitOrPush": true
  }
}
JSON

echo "===== build ReleaseEvidence ====="
RELEASE_REPORT_DIR="$REPORT_DIR" \
./scripts/build-release-evidence.sh \
  "$REPORT_DIR/ai-decision-$RELEASE_ID.json" \
  "$REPORT_DIR/policy-decision-$RELEASE_ID.json" \
  > "$TMP_DIR/build-release-evidence.log" 2>&1

EVIDENCE="$REPORT_DIR/release-evidence-$RELEASE_ID.json"
test -f "$EVIDENCE"

echo "===== assert ReleaseEvidence trace fields ====="
python3 - "$EVIDENCE" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))

assert doc["releaseId"] == "20260101-000000", doc
assert doc["traceId"] == "trace-20260101-000000", doc
assert doc["agentTraceId"] == "at-20260101-000000", doc
assert doc["rootSpanId"] == "span-agent-root-20260101-000000", doc

assert doc["artifacts"]["agentTrace"].endswith("agent-trace-20260101-000000.json"), doc["artifacts"]
assert doc["artifacts"]["otelSpanBundle"].endswith("otel-span-bundle-20260101-000000.json"), doc["artifacts"]

obs = doc["observability"]
assert obs["traceId"] == doc["traceId"], obs
assert obs["agentTraceId"] == doc["agentTraceId"], obs
assert obs["rootSpanId"] == doc["rootSpanId"], obs
assert obs["localFileOnly"] is True, obs
assert obs["doesNotSendExternalTelemetry"] is True, obs
assert obs["doesNotCallExternalCollector"] is True, obs

print("PASS: ReleaseEvidence carries traceId and local OTel refs")
PY

echo "PASS: Agent Trace and OTel ReleaseEvidence traceId test passed"
