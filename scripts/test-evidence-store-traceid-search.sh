#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-evidence-store-traceid-search}"
REPORT_DIR="$TMP_DIR/reports"
DB_FILE="$TMP_DIR/evidence-store.db"
RELEASE_ID="20260101-000000"
TRACE_ID="trace-$RELEASE_ID"

rm -rf "$TMP_DIR"
mkdir -p "$REPORT_DIR"

echo "===== syntax checks ====="
python3 -m py_compile scripts/evidence-store.py
bash -n scripts/test-evidence-store-traceid-search.sh

cat > "$REPORT_DIR/release-evidence-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-evidence-store-traceid-search.sh",
  "releaseId": "$RELEASE_ID",
  "traceId": "$TRACE_ID",
  "agentTraceId": "at-$RELEASE_ID",
  "rootSpanId": "span-agent-root-$RELEASE_ID",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "summary": {
    "riskLevel": "critical",
    "riskScore": 100
  },
  "artifacts": {
    "agentTrace": "agent-trace-$RELEASE_ID.json",
    "otelSpanBundle": "otel-span-bundle-$RELEASE_ID.json"
  },
  "observability": {
    "traceId": "$TRACE_ID",
    "agentTraceId": "at-$RELEASE_ID",
    "rootSpanId": "span-agent-root-$RELEASE_ID",
    "agentTrace": "agent-trace-$RELEASE_ID.json",
    "otelSpanBundle": "otel-span-bundle-$RELEASE_ID.json",
    "localFileOnly": true,
    "doesNotSendExternalTelemetry": true,
    "doesNotCallExternalCollector": true
  }
}
JSON

cat > "$REPORT_DIR/evidence-record-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "evidence.record/v1alpha1",
  "generatedBy": "test-evidence-store-traceid-search.sh",
  "evidenceId": "ev-$RELEASE_ID-demo-app-dev",
  "releaseId": "$RELEASE_ID",
  "traceId": "$TRACE_ID",
  "agentTraceId": "at-$RELEASE_ID",
  "rootSpanId": "span-agent-root-$RELEASE_ID",
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "summary": {
    "riskLevel": "critical",
    "riskScore": 100
  },
  "links": {
    "releaseEvidence": "release-evidence-$RELEASE_ID.json",
    "agentTrace": "agent-trace-$RELEASE_ID.json",
    "otelSpanBundle": "otel-span-bundle-$RELEASE_ID.json"
  },
  "observability": {
    "traceId": "$TRACE_ID",
    "agentTraceId": "at-$RELEASE_ID",
    "rootSpanId": "span-agent-root-$RELEASE_ID",
    "agentTrace": "agent-trace-$RELEASE_ID.json",
    "otelSpanBundle": "otel-span-bundle-$RELEASE_ID.json",
    "localFileOnly": true,
    "doesNotSendExternalTelemetry": true,
    "doesNotCallExternalCollector": true
  }
}
JSON

cat > "$REPORT_DIR/agent-trace-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "agent.trace/v1alpha1",
  "generatedBy": "test-evidence-store-traceid-search.sh",
  "agentTraceId": "at-$RELEASE_ID",
  "traceId": "$TRACE_ID",
  "releaseId": "$RELEASE_ID",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "namespace": "slo-rollout",
    "env": "dev"
  }
}
JSON

cat > "$REPORT_DIR/otel-span-bundle-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "otel.span.bundle/v1alpha1",
  "kind": "OtelSpanBundle",
  "traceId": "$TRACE_ID",
  "rootSpanId": "span-agent-root-$RELEASE_ID",
  "releaseId": "$RELEASE_ID",
  "generatedBy": "test-evidence-store-traceid-search.sh",
  "source": {
    "kind": "AgentTrace",
    "schemaVersion": "agent.trace/v1alpha1",
    "agentTraceId": "at-$RELEASE_ID",
    "path": "agent-trace-$RELEASE_ID.json"
  },
  "resource": {
    "service": "demo-app",
    "namespace": "slo-rollout",
    "env": "dev"
  },
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

echo "===== import trace objects ====="
./scripts/evidence-store.py init-db --db "$DB_FILE" >/dev/null
./scripts/evidence-store.py import-dir --db "$DB_FILE" --report-dir "$REPORT_DIR" > "$TMP_DIR/import.json"

./scripts/evidence-store.py search-objects \
  --db "$DB_FILE" \
  --query "$TRACE_ID" \
  --limit 20 \
  > "$TMP_DIR/search.json"

./scripts/evidence-store.py query-release \
  --db "$DB_FILE" \
  --release-id "$RELEASE_ID" \
  > "$TMP_DIR/query-release.json"

echo "===== assert trace summary/search ====="
python3 - "$TMP_DIR/import.json" "$TMP_DIR/search.json" "$TMP_DIR/query-release.json" <<'PY'
import json
import sys
from pathlib import Path

import_result = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
search = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
release = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))

expected = {"releaseEvidence", "evidenceRecord", "agentTrace", "otelSpanBundle"}

for kind in expected:
    assert import_result["byType"].get(kind) == 1, import_result

found = {item["object_type"] for item in search["items"]}
assert expected.issubset(found), found

summaries = {
    item["object_type"]: item["summary"]
    for item in release["objects"]
    if item["object_type"] in expected
}

for kind in expected:
    assert summaries[kind]["traceId"] == "trace-20260101-000000", summaries[kind]

assert summaries["releaseEvidence"]["agentTraceId"] == "at-20260101-000000", summaries["releaseEvidence"]
assert summaries["evidenceRecord"]["rootSpanId"] == "span-agent-root-20260101-000000", summaries["evidenceRecord"]
assert summaries["otelSpanBundle"]["sourceAgentTraceId"] == "at-20260101-000000", summaries["otelSpanBundle"]

assert summaries["releaseEvidence"]["doesNotSendExternalTelemetry"] is True, summaries["releaseEvidence"]
assert summaries["evidenceRecord"]["doesNotCallExternalCollector"] is True, summaries["evidenceRecord"]

print("PASS: EvidenceStore summary/search exposes full traceId chain")
PY

echo "PASS: Agent Trace and OTel EvidenceStore traceId search test passed"
