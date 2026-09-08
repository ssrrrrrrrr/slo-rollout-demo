#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-otel-evidence-store}"
REPORT_DIR="$TMP_DIR/reports"
DB_FILE="$TMP_DIR/evidence-store.db"
IMPORT_JSON="$TMP_DIR/import.json"
OBJECT_JSON="$TMP_DIR/otel-span-bundle-object.json"

RELEASE_ID="20260101-000000"
TRACE_ID="trace-$RELEASE_ID"
ROOT_SPAN_ID="span-agent-root-$RELEASE_ID"

rm -rf "$TMP_DIR"
mkdir -p "$REPORT_DIR"

echo "===== syntax checks ====="
python3 -m py_compile scripts/evidence-store.py
bash -n scripts/test-otel-evidence-store.sh

echo "===== create OTel span bundle fixture ====="
cat > "$REPORT_DIR/otel-span-bundle-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "otel.span.bundle/v1alpha1",
  "kind": "OtelSpanBundle",
  "traceId": "$TRACE_ID",
  "rootSpanId": "$ROOT_SPAN_ID",
  "releaseId": "$RELEASE_ID",
  "generatedBy": "test-otel-evidence-store.sh",
  "generatedAt": "2026-01-01T00:00:00Z",
  "source": {
    "kind": "AgentTrace",
    "schemaVersion": "agent.trace/v1alpha1",
    "agentTraceId": "at-$RELEASE_ID",
    "path": "agent-trace-$RELEASE_ID.json"
  },
  "resource": {
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "version": "v47",
    "commit": "abc123",
    "imageDigest": "sha256:111"
  },
  "spans": [
    {
      "traceId": "$TRACE_ID",
      "spanId": "$ROOT_SPAN_ID",
      "parentSpanId": null,
      "name": "ssentinel.agent.run",
      "kind": "internal",
      "startTime": "2026-01-01T00:00:00Z",
      "endTime": "2026-01-01T00:00:01Z",
      "status": {
        "code": "OK"
      },
      "attributes": {
        "ssentinel.release_id": "$RELEASE_ID",
        "ssentinel.agent_trace_id": "at-$RELEASE_ID",
        "ssentinel.agent_run_id": "ar-$RELEASE_ID"
      },
      "events": [],
      "links": []
    },
    {
      "traceId": "$TRACE_ID",
      "spanId": "span-policy-$RELEASE_ID",
      "parentSpanId": "$ROOT_SPAN_ID",
      "name": "ssentinel.policy.evaluate",
      "kind": "internal",
      "startTime": "2026-01-01T00:00:00Z",
      "endTime": "2026-01-01T00:00:01Z",
      "status": {
        "code": "OK"
      },
      "attributes": {
        "ssentinel.policy_decision": "REQUIRE_HUMAN_APPROVAL"
      },
      "events": [],
      "links": []
    }
  ],
  "summary": {
    "spanCount": 2,
    "hasRootSpan": true,
    "sourceAgentTraceId": "at-$RELEASE_ID",
    "releaseId": "$RELEASE_ID",
    "spanNames": [
      "ssentinel.agent.run",
      "ssentinel.policy.evaluate"
    ]
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

cp "$REPORT_DIR/otel-span-bundle-$RELEASE_ID.json" "$REPORT_DIR/otel-span-bundle-latest.json"

echo "===== import OTel span bundle into EvidenceStore ====="
./scripts/evidence-store.py init-db --db "$DB_FILE" >/dev/null
./scripts/evidence-store.py import-dir --db "$DB_FILE" --report-dir "$REPORT_DIR" > "$IMPORT_JSON"

echo "===== get OTel span bundle object ====="
./scripts/evidence-store.py get-object \
  --db "$DB_FILE" \
  --object-type otelSpanBundle \
  --object-id "$TRACE_ID" \
  --release-id "$RELEASE_ID" \
  > "$OBJECT_JSON"

echo "===== assert EvidenceStore OTel indexing ====="
python3 - "$IMPORT_JSON" "$OBJECT_JSON" <<'PY'
import json
import sys
from pathlib import Path

import_result = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
obj = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))

assert import_result["byType"]["otelSpanBundle"] == 1, import_result
assert import_result["importedObjects"] == 1, import_result
assert import_result["releaseCount"] == 1, import_result

assert obj["schemaVersion"] == "evidence.store.object/v1alpha1", obj

release = obj["release"]
assert release["release_id"] == "20260101-000000", release
assert release["service"] == "demo-app", release
assert release["env"] == "dev", release
assert release["namespace"] == "slo-rollout", release
assert release["version"] == "v47", release
assert release["commit_sha"] == "abc123", release
assert release["image_digest"] == "sha256:111", release

stored = obj["object"]
assert stored["object_type"] == "otelSpanBundle", stored
assert stored["object_id"] == "trace-20260101-000000", stored
assert stored["release_id"] == "20260101-000000", stored
assert stored["schema_version"] == "otel.span.bundle/v1alpha1", stored

summary = stored["summary"]
assert summary["objectType"] == "otelSpanBundle", summary
assert summary["schemaVersion"] == "otel.span.bundle/v1alpha1", summary
assert summary["traceId"] == "trace-20260101-000000", summary
assert summary["rootSpanId"] == "span-agent-root-20260101-000000", summary
assert summary["spanCount"] == 2, summary
assert summary["hasRootSpan"] is True, summary
assert summary["sourceAgentTraceId"] == "at-20260101-000000", summary
assert summary["doesNotSendExternalTelemetry"] is True, summary
assert summary["doesNotCallExternalCollector"] is True, summary
assert summary["localFileOnly"] is True, summary
assert "ssentinel.agent.run" in summary["spanNames"], summary

print("PASS: OTel span bundle is indexed by EvidenceStore")
PY

echo "PASS: Agent Trace and OTel OTel EvidenceStore indexing test passed"
