#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

SCHEMA="schemas/otel-span-bundle.schema.json"
TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-otel-span-bundle-schema}"
BUNDLE="$TMP_DIR/otel-span-bundle.json"

rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

echo "===== syntax check ====="
python3 -m json.tool "$SCHEMA" >/dev/null

echo "===== create sample OTel span bundle ====="
cat > "$BUNDLE" <<'JSON'
{
  "schemaVersion": "otel.span.bundle/v1alpha1",
  "kind": "OtelSpanBundle",
  "traceId": "trace-20260101-000000",
  "rootSpanId": "span-agent-root-20260101-000000",
  "releaseId": "20260101-000000",
  "generatedBy": "test-otel-span-bundle-schema.sh",
  "generatedAt": "2026-01-01T00:00:00Z",
  "source": {
    "kind": "AgentTrace",
    "schemaVersion": "agent.trace/v1alpha1",
    "agentTraceId": "at-20260101-000000",
    "path": "/tmp/agent-trace-20260101-000000.json"
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
      "traceId": "trace-20260101-000000",
      "spanId": "span-agent-root-20260101-000000",
      "parentSpanId": null,
      "name": "ssentinel.agent.run",
      "kind": "internal",
      "startTime": "2026-01-01T00:00:00Z",
      "endTime": "2026-01-01T00:00:01Z",
      "status": {
        "code": "OK"
      },
      "attributes": {
        "ssentinel.release_id": "20260101-000000",
        "ssentinel.agent_trace_id": "at-20260101-000000",
        "ssentinel.agent_run_id": "ar-20260101-000000"
      },
      "events": [],
      "links": []
    },
    {
      "traceId": "trace-20260101-000000",
      "spanId": "span-policy-20260101-000000",
      "parentSpanId": "span-agent-root-20260101-000000",
      "name": "ssentinel.policy.evaluate",
      "kind": "internal",
      "startTime": "2026-01-01T00:00:00Z",
      "endTime": "2026-01-01T00:00:01Z",
      "status": {
        "code": "OK"
      },
      "attributes": {
        "ssentinel.policy_decision_id": "pd-20260101-000000",
        "ssentinel.policy_decision": "REQUIRE_HUMAN_APPROVAL",
        "ssentinel.final_action": "STOP_PROMOTION"
      },
      "events": [],
      "links": []
    }
  ],
  "summary": {
    "spanCount": 2,
    "hasRootSpan": true,
    "sourceAgentTraceId": "at-20260101-000000",
    "releaseId": "20260101-000000",
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

echo "===== validate OTel span bundle schema contract ====="
python3 - "$SCHEMA" "$BUNDLE" <<'PY'
import json
import sys
from pathlib import Path

schema_path = Path(sys.argv[1])
bundle_path = Path(sys.argv[2])

try:
    import jsonschema
except Exception as exc:
    raise SystemExit(f"jsonschema is required: {exc}")

schema = json.loads(schema_path.read_text(encoding="utf-8"))
bundle = json.loads(bundle_path.read_text(encoding="utf-8"))

validator_cls = jsonschema.validators.validator_for(schema)
validator_cls.check_schema(schema)
validator = validator_cls(schema)
errors = sorted(validator.iter_errors(bundle), key=lambda e: list(e.path))
if errors:
    for error in errors:
        location = ".".join(str(p) for p in error.path) or "<root>"
        print(f"FAIL: {location}: {error.message}", file=sys.stderr)
    raise SystemExit(1)

assert schema["properties"]["schemaVersion"]["const"] == "otel.span.bundle/v1alpha1", schema
assert bundle["schemaVersion"] == "otel.span.bundle/v1alpha1", bundle
assert bundle["kind"] == "OtelSpanBundle", bundle
assert bundle["source"]["kind"] == "AgentTrace", bundle
assert bundle["source"]["schemaVersion"] == "agent.trace/v1alpha1", bundle
assert bundle["traceId"] == "trace-20260101-000000", bundle
assert bundle["rootSpanId"] == "span-agent-root-20260101-000000", bundle

spans = bundle["spans"]
span_ids = {span["spanId"] for span in spans}
assert bundle["rootSpanId"] in span_ids, spans
assert sum(1 for span in spans if span["parentSpanId"] is None) == 1, spans
assert all(span["traceId"] == bundle["traceId"] for span in spans), spans

for span in spans:
    parent = span["parentSpanId"]
    if parent is not None:
        assert parent in span_ids, span

assert bundle["summary"]["spanCount"] == len(spans), bundle["summary"]
assert bundle["summary"]["hasRootSpan"] is True, bundle["summary"]
assert bundle["summary"]["sourceAgentTraceId"] == bundle["source"]["agentTraceId"], bundle["summary"]
assert bundle["summary"]["releaseId"] == bundle["releaseId"], bundle["summary"]

guardrails = bundle["guardrails"]
assert guardrails["localFileOnly"] is True, guardrails
assert guardrails["doesNotSendExternalTelemetry"] is True, guardrails
assert guardrails["doesNotCallExternalCollector"] is True, guardrails
assert guardrails["doesNotModifyCluster"] is True, guardrails

print("PASS: OTel span bundle schema contract is valid")
PY

echo "PASS: Agent Trace and OTel OTel span bundle schema test passed"
