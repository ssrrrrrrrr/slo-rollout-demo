#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-agent-trace-to-otel}"
TRACE_DIR="$TMP_DIR/trace"
OTEL_DIR="$TMP_DIR/otel"

rm -rf "$TMP_DIR"
mkdir -p "$TRACE_DIR" "$OTEL_DIR"

AI_DECISION="$TRACE_DIR/ai-decision-20260101-000000.json"
POLICY_DECISION="$TRACE_DIR/policy-decision-20260101-000000.json"
POLICY_RUNTIME="$TRACE_DIR/policy-runtime-result-20260101-000000.json"
SIGNED_GATE="$TRACE_DIR/signed-release-gate-20260101-000000.json"
RELEASE_EVIDENCE="$TRACE_DIR/release-evidence-20260101-000000.json"
AGENT_TRACE="$TRACE_DIR/agent-trace-20260101-000000.json"
OTEL_BUNDLE="$OTEL_DIR/otel-span-bundle-20260101-000000.json"
OTEL_LATEST="$OTEL_DIR/otel-span-bundle-latest.json"

echo "===== syntax checks ====="
python3 -m py_compile scripts/agent-trace-to-otel.py
bash -n scripts/build-agent-otel-spans.sh
bash -n scripts/test-agent-trace-otel.sh

echo "===== create AgentTrace source fixtures ====="
cat > "$AI_DECISION" <<'JSON'
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-agent-trace-otel.sh",
  "generatedAt": "2026-01-01T00:00:00Z",
  "releaseId": "20260101-000000",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "recommendedAction": "STOP_PROMOTION",
  "requiresHumanApproval": true,
  "service": "demo-app",
  "env": "dev",
  "version": "v47",
  "commit": "abc123",
  "evidence": {
    "releaseId": "20260101-000000",
    "service": "demo-app",
    "env": "dev",
    "image": "registry.local/demo-app@sha256:111",
    "imageDigest": "sha256:111"
  }
}
JSON

cat > "$POLICY_DECISION" <<'JSON'
{
  "schemaVersion": "release.policy.evaluator/v1alpha1",
  "policyDecisionId": "pd-20260101-000000",
  "releaseId": "20260101-000000",
  "service": "demo-app",
  "env": "dev",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "requestedAction": "STOP_PROMOTION",
  "allowed": true,
  "finalAction": "STOP_PROMOTION",
  "requiresHumanApproval": true,
  "matchedRules": [
    "signed_release_gate_requires_human_approval"
  ],
  "safety": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$POLICY_RUNTIME" <<'JSON'
{
  "schemaVersion": "policy.runtime.result/v1alpha1",
  "generatedBy": "test-agent-trace-otel.sh",
  "generatedAt": "2026-01-01T00:00:01Z",
  "runtime": {
    "name": "local-python",
    "adapter": "evaluate-agent-decision.sh",
    "mode": "subprocess"
  },
  "summary": {
    "policyDecision": "REQUIRE_HUMAN_APPROVAL",
    "finalAction": "STOP_PROMOTION",
    "allowed": true,
    "requiresHumanApproval": true,
    "matchedRules": [
      "signed_release_gate_requires_human_approval"
    ]
  },
  "safety": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$SIGNED_GATE" <<'JSON'
{
  "schemaVersion": "signed.release.gate/v1alpha1",
  "signedReleaseGateId": "srg-20260101-000000",
  "generatedBy": "test-agent-trace-otel.sh",
  "generatedAt": "2026-01-01T00:00:02Z",
  "release": {
    "releaseId": "20260101-000000",
    "service": "demo-app",
    "env": "dev"
  },
  "image": {
    "image": "registry.local/demo-app@sha256:111",
    "imageDigest": "sha256:111"
  },
  "decision": {
    "decision": "REQUIRE_HUMAN_APPROVAL",
    "allowed": false,
    "requiresHumanApproval": true
  },
  "risk": {
    "riskLevel": "high",
    "riskScore": 50
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$RELEASE_EVIDENCE" <<'JSON'
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-agent-trace-otel.sh",
  "generatedAt": "2026-01-01T00:00:03Z",
  "releaseId": "20260101-000000",
  "service": "demo-app",
  "env": "dev",
  "namespace": "slo-rollout",
  "version": "v47",
  "commit": "abc123",
  "imageDigest": "sha256:111",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION"
}
JSON

echo "===== build AgentTrace ====="
AGENT_TRACE_OUTPUT_DIR="$TRACE_DIR" ./scripts/build-agent-trace.sh "$AI_DECISION" > "$TMP_DIR/build-agent-trace.json"

test -f "$AGENT_TRACE"
test -f "$TRACE_DIR/agent-trace-latest.json"

echo "===== convert AgentTrace to OTel span bundle ====="
RELEASE_REPORT_DIR="$TRACE_DIR" \
AGENT_OTEL_OUTPUT_DIR="$OTEL_DIR" \
./scripts/build-agent-otel-spans.sh "$AGENT_TRACE" > "$TMP_DIR/build-agent-otel-spans.json"

test -f "$OTEL_BUNDLE"
test -f "$OTEL_LATEST"

echo "===== assert OTel span bundle contract ====="
python3 - \
  "$OTEL_BUNDLE" \
  "$OTEL_LATEST" \
  "$TMP_DIR/build-agent-otel-spans.json" \
  schemas/otel-span-bundle.schema.json <<'PY'
import json
import sys
from pathlib import Path

bundle = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
latest = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
build = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
schema = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))

try:
    import jsonschema
except Exception as exc:
    raise SystemExit(f"jsonschema is required: {exc}")

validator_cls = jsonschema.validators.validator_for(schema)
validator_cls.check_schema(schema)
validator = validator_cls(schema)

errors = sorted(validator.iter_errors(bundle), key=lambda e: list(e.path))
if errors:
    for error in errors:
        location = ".".join(str(p) for p in error.path) or "<root>"
        print(f"FAIL: {location}: {error.message}", file=sys.stderr)
    raise SystemExit(1)

assert latest == bundle, latest
assert bundle["schemaVersion"] == "otel.span.bundle/v1alpha1", bundle
assert bundle["kind"] == "OtelSpanBundle", bundle
assert bundle["traceId"] == "trace-20260101-000000", bundle
assert bundle["releaseId"] == "20260101-000000", bundle
assert bundle["source"]["kind"] == "AgentTrace", bundle["source"]
assert bundle["source"]["schemaVersion"] == "agent.trace/v1alpha1", bundle["source"]
assert bundle["source"]["agentTraceId"] == "at-20260101-000000", bundle["source"]

assert bundle["resource"]["service"] == "demo-app", bundle["resource"]
assert bundle["resource"]["env"] == "dev", bundle["resource"]
assert bundle["resource"]["namespace"] == "slo-rollout", bundle["resource"]
assert bundle["resource"]["version"] == "v47", bundle["resource"]
assert bundle["resource"]["commit"] == "abc123", bundle["resource"]
assert bundle["resource"]["imageDigest"] == "sha256:111", bundle["resource"]

spans = bundle["spans"]
span_ids = {span["spanId"] for span in spans}
span_names = {span["name"] for span in spans}

assert bundle["rootSpanId"] in span_ids, spans
assert sum(1 for span in spans if span["parentSpanId"] is None) == 1, spans
assert all(span["traceId"] == bundle["traceId"] for span in spans), spans

for span in spans:
    parent = span["parentSpanId"]
    if parent is not None:
        assert parent in span_ids, span

expected_names = {
    "ssentinel.agent.run",
    "ssentinel.policy.evaluate",
    "ssentinel.signed_release_gate.evaluate",
    "ssentinel.tool_call.ai_release_advisor",
    "ssentinel.tool_call.policy_runtime_adapter",
    "ssentinel.tool_call.policy_guard",
    "ssentinel.tool_call.signed_release_gate",
    "ssentinel.tool_call.release_evidence",
    "ssentinel.evidence.link"
}
assert expected_names.issubset(span_names), span_names

root_span = next(span for span in spans if span["spanId"] == bundle["rootSpanId"])
assert root_span["attributes"]["ssentinel.release_id"] == "20260101-000000", root_span
assert root_span["attributes"]["ssentinel.agent_trace_id"] == "at-20260101-000000", root_span
assert root_span["attributes"]["ssentinel.agent_run_id"] == "ar-20260101-000000", root_span

policy_span = next(span for span in spans if span["name"] == "ssentinel.policy.evaluate")
assert policy_span["attributes"]["ssentinel.policy_decision"] == "REQUIRE_HUMAN_APPROVAL", policy_span
assert policy_span["attributes"]["ssentinel.final_action"] == "STOP_PROMOTION", policy_span
assert policy_span["attributes"]["ssentinel.policy_runtime_name"] == "local-python", policy_span

assert bundle["summary"]["spanCount"] == len(spans), bundle["summary"]
assert bundle["summary"]["hasRootSpan"] is True, bundle["summary"]
assert bundle["summary"]["sourceAgentTraceId"] == "at-20260101-000000", bundle["summary"]
assert bundle["summary"]["releaseId"] == "20260101-000000", bundle["summary"]

guardrails = bundle["guardrails"]
assert guardrails["localFileOnly"] is True, guardrails
assert guardrails["doesNotSendExternalTelemetry"] is True, guardrails
assert guardrails["doesNotCallExternalCollector"] is True, guardrails
assert guardrails["doesNotModifyCluster"] is True, guardrails
assert guardrails["doesNotModifyGitOps"] is True, guardrails
assert guardrails["doesNotCommitOrPush"] is True, guardrails

assert build["schemaVersion"] == "otel.span.bundle.build/v1alpha1", build
assert build["traceId"] == bundle["traceId"], build
assert build["rootSpanId"] == bundle["rootSpanId"], build
assert build["spanCount"] == bundle["summary"]["spanCount"], build
assert build["guardrails"]["doesNotSendExternalTelemetry"] is True, build

print("PASS: AgentTrace was converted to OTel span bundle")
PY

echo "PASS: Agent Trace and OTel AgentTrace to OTel span bundle test passed"
