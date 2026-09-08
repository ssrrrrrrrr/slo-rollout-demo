#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-agent-trace-otel-agent-trace-otel-integration}"
OK_DIR="$TMP_DIR/ok"
FAIL_DIR="$TMP_DIR/fail"
RELEASE_ID="20260101-000000"

rm -rf "$TMP_DIR"
mkdir -p "$OK_DIR" "$FAIL_DIR"

echo "===== syntax checks ====="
bash -n scripts/build-agent-trace.sh
bash -n scripts/build-agent-otel-spans.sh
bash -n scripts/test-agent-trace-otel-integration.sh
python3 -m py_compile scripts/agent-trace-to-otel.py

create_fixtures() {
  local dir="$1"

  cat > "$dir/ai-decision-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "ai.release.advisor/v1alpha1",
  "generatedBy": "test-agent-trace-otel-integration.sh",
  "generatedAt": "2026-01-01T00:00:00Z",
  "releaseId": "$RELEASE_ID",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "recommendedAction": "STOP_PROMOTION",
  "requiresHumanApproval": true,
  "service": "demo-app",
  "env": "dev",
  "version": "v47",
  "commit": "abc123",
  "evidence": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "imageDigest": "sha256:111"
  }
}
JSON

  cat > "$dir/policy-decision-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.policy.evaluator/v1alpha1",
  "policyDecisionId": "pd-$RELEASE_ID",
  "releaseId": "$RELEASE_ID",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "allowed": true,
  "finalAction": "STOP_PROMOTION",
  "requiresHumanApproval": true,
  "matchedRules": ["signed_release_gate_requires_human_approval"]
}
JSON

  cat > "$dir/policy-runtime-result-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "policy.runtime.result/v1alpha1",
  "generatedBy": "test-agent-trace-otel-integration.sh",
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
    "matchedRules": ["signed_release_gate_requires_human_approval"]
  }
}
JSON

  cat > "$dir/signed-release-gate-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "signed.release.gate/v1alpha1",
  "signedReleaseGateId": "srg-$RELEASE_ID",
  "generatedBy": "test-agent-trace-otel-integration.sh",
  "generatedAt": "2026-01-01T00:00:02Z",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout"
  },
  "image": {
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

  cat > "$dir/release-evidence-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-agent-trace-otel-integration.sh",
  "generatedAt": "2026-01-01T00:00:03Z",
  "releaseId": "$RELEASE_ID",
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
}

echo "===== assert build-agent-trace auto-generates OTel span bundle ====="
create_fixtures "$OK_DIR"

AGENT_TRACE_OUTPUT_DIR="$OK_DIR" \
./scripts/build-agent-trace.sh "$OK_DIR/ai-decision-$RELEASE_ID.json" \
  > "$OK_DIR/build-agent-trace.stdout" \
  2> "$OK_DIR/build-agent-trace.stderr"

test -f "$OK_DIR/agent-trace-$RELEASE_ID.json"
test -f "$OK_DIR/agent-trace-latest.json"
test -f "$OK_DIR/otel-span-bundle-$RELEASE_ID.json"
test -f "$OK_DIR/otel-span-bundle-latest.json"

python3 - "$OK_DIR/otel-span-bundle-$RELEASE_ID.json" schemas/otel-span-bundle.schema.json <<'PY'
import json
import sys
from pathlib import Path

bundle = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
schema = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))

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

assert bundle["schemaVersion"] == "otel.span.bundle/v1alpha1", bundle
assert bundle["traceId"] == "trace-20260101-000000", bundle
assert bundle["releaseId"] == "20260101-000000", bundle
assert bundle["source"]["agentTraceId"] == "at-20260101-000000", bundle
assert bundle["guardrails"]["localFileOnly"] is True, bundle
assert bundle["guardrails"]["doesNotSendExternalTelemetry"] is True, bundle
assert bundle["guardrails"]["doesNotCallExternalCollector"] is True, bundle
assert "ssentinel.agent.run" in bundle["summary"]["spanNames"], bundle["summary"]

print("PASS: build-agent-trace generated local OTel span bundle")
PY

echo "===== assert OTel generation failure does not block AgentTrace ====="
create_fixtures "$FAIL_DIR"

set +e
PYTHON_BIN="/tmp/ssentinel-missing-python" \
AGENT_TRACE_OUTPUT_DIR="$FAIL_DIR" \
./scripts/build-agent-trace.sh "$FAIL_DIR/ai-decision-$RELEASE_ID.json" \
  > "$FAIL_DIR/build-agent-trace.stdout" \
  2> "$FAIL_DIR/build-agent-trace.stderr"
STATUS=$?
set -e

if [ "$STATUS" -ne 0 ]; then
  echo "FAIL: build-agent-trace.sh should not fail when OTel builder fails" >&2
  cat "$FAIL_DIR/build-agent-trace.stderr" >&2 || true
  exit 1
fi

test -f "$FAIL_DIR/agent-trace-$RELEASE_ID.json"
test -f "$FAIL_DIR/agent-trace-latest.json"
grep -q "WARN: build-agent-otel-spans.sh failed, continue agent trace pipeline" "$FAIL_DIR/build-agent-trace.stderr"

echo "PASS: OTel generation failure is warning-only"
echo "PASS: Agent Trace and OTel AgentTrace OTel integration test passed"
