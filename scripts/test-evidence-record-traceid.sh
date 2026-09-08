#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-evidence-record-traceid}"
RELEASE_ID="20260101-000000"

rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

echo "===== syntax checks ====="
bash -n scripts/build-evidence-record.sh
bash -n scripts/test-evidence-record-traceid.sh
python3 - <<'PY'
import json
from pathlib import Path
json.loads(Path("schemas/evidence-record.schema.json").read_text(encoding="utf-8"))
print("PASS: evidence record schema parses")
PY

cat > "$TMP_DIR/release-context-$RELEASE_ID.json" <<JSON
{
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "currentDesiredVersion": "v47-trace"
}
JSON

cat > "$TMP_DIR/agent-trace-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "agent.trace/v1alpha1",
  "agentTraceId": "at-$RELEASE_ID",
  "traceId": "trace-$RELEASE_ID",
  "releaseId": "$RELEASE_ID"
}
JSON

cat > "$TMP_DIR/otel-span-bundle-$RELEASE_ID.json" <<JSON
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

cat > "$TMP_DIR/release-evidence-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-evidence-record-traceid.sh",
  "releaseId": "$RELEASE_ID",
  "traceId": "trace-$RELEASE_ID",
  "agentTraceId": "at-$RELEASE_ID",
  "rootSpanId": "span-agent-root-$RELEASE_ID",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "executionMode": "advisory_only",
  "requiresHumanApproval": true,
  "safeToRetry": false,
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "summary": {
    "riskLevel": "critical",
    "riskScore": 100,
    "failedMetrics": ["error-rate"]
  },
  "artifacts": {
    "releaseContext": "$TMP_DIR/release-context-$RELEASE_ID.json",
    "aiDecision": "$TMP_DIR/ai-decision-$RELEASE_ID.json",
    "policyDecision": "$TMP_DIR/policy-decision-$RELEASE_ID.json",
    "agentTrace": "$TMP_DIR/agent-trace-$RELEASE_ID.json",
    "otelSpanBundle": "$TMP_DIR/otel-span-bundle-$RELEASE_ID.json"
  },
  "observability": {
    "traceId": "trace-$RELEASE_ID",
    "agentTraceId": "at-$RELEASE_ID",
    "rootSpanId": "span-agent-root-$RELEASE_ID",
    "localFileOnly": true,
    "doesNotSendExternalTelemetry": true,
    "doesNotCallExternalCollector": true
  }
}
JSON

touch "$TMP_DIR/ai-decision-$RELEASE_ID.json" "$TMP_DIR/policy-decision-$RELEASE_ID.json"

echo "===== build EvidenceRecord ====="
EVIDENCE_RECORD_OUTPUT_DIR="$TMP_DIR" \
scripts/build-evidence-record.sh "$TMP_DIR/release-evidence-$RELEASE_ID.json" \
  > "$TMP_DIR/build-evidence-record.log" 2>&1

RECORD="$TMP_DIR/evidence-record-$RELEASE_ID.json"
test -f "$RECORD"

echo "===== assert EvidenceRecord trace fields ====="
python3 - "$RECORD" <<'PY'
import json
import sys
from pathlib import Path

record = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))

assert record["traceId"] == "trace-20260101-000000", record
assert record["agentTraceId"] == "at-20260101-000000", record
assert record["rootSpanId"] == "span-agent-root-20260101-000000", record

obs = record["observability"]
assert obs["traceId"] == record["traceId"], obs
assert obs["agentTraceId"] == record["agentTraceId"], obs
assert obs["rootSpanId"] == record["rootSpanId"], obs
assert obs["localFileOnly"] is True, obs
assert obs["doesNotSendExternalTelemetry"] is True, obs
assert obs["doesNotCallExternalCollector"] is True, obs

assert record["links"]["agentTrace"].endswith("agent-trace-20260101-000000.json"), record["links"]
assert record["links"]["otelSpanBundle"].endswith("otel-span-bundle-20260101-000000.json"), record["links"]
assert record["artifacts"]["agentTrace"]["exists"] is True, record["artifacts"]["agentTrace"]
assert record["artifacts"]["otelSpanBundle"]["exists"] is True, record["artifacts"]["otelSpanBundle"]

print("PASS: EvidenceRecord inherits traceId and local OTel refs")
PY

python3 scripts/validate-release-contracts.py "$RECORD"

echo "PASS: Agent Trace and OTel EvidenceRecord traceId test passed"
