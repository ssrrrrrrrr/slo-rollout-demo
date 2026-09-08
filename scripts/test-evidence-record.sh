#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TEST_TMP="$(mktemp -d)"
trap 'rm -rf "$TEST_TMP"' EXIT

log() {
  echo "===== $* ====="
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

log "syntax checks"
bash -n scripts/build-evidence-record.sh
bash -n scripts/ai-release-advisor.sh

log "schema parse checks"
python3 - <<'PY'
import json
from pathlib import Path

for item in [
    "schemas/evidence-record.schema.json",
    "schemas/release-evidence.schema.json",
    "schemas/ai-decision.schema.json",
]:
    json.loads(Path(item).read_text(encoding="utf-8"))
    print(f"PASS: {item}")
PY

log "create fixture files"

cat > "$TEST_TMP/release-context-test.json" <<JSON
{
  "generatedAt": "2026-05-21T10:00:00Z",
  "namespace": "slo-rollout",
  "service": "demo-app",
  "env": "dev",
  "sloId": "demo-app-canary-slo",
  "sloConfigRef": "configs/services/demo-app.slo.yaml",
  "strategyId": "demo-app-canary-strategy",
  "strategyConfigRef": "configs/services/demo-app.strategy.yaml",
  "rollout": "demo-app",
  "rolloutPhase": "Degraded",
  "rolloutAbort": true,
  "currentDesiredVersion": "v29-evidence-record",
  "analysisRun": "demo-app-test",
  "analysisRunPhase": "Failed",
  "failedMetrics": ["error-rate", "p95-latency"],
  "analysisRunMetrics": [],
  "severity": "critical",
  "riskScore": 100,
  "riskReasons": ["multiple SLO gates failed"]
}
JSON

cat > "$TEST_TMP/release-evidence-20260521-100000.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "build-release-evidence.sh",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "ALLOW_ADVISORY_ONLY",
  "finalAction": "STOP_PROMOTION",
  "executionMode": "advisory_only",
  "requiresHumanApproval": true,
  "safeToRetry": false,
  "service": "demo-app",
  "env": "dev",
  "sloId": "demo-app-canary-slo",
  "sloConfigRef": "configs/services/demo-app.slo.yaml",
  "sloConfigSnapshot": {
    "apiVersion": "slo.ssentinel.io/v1alpha1",
    "kind": "SLOConfig",
    "metadata": {
      "name": "demo-app-canary-slo"
    },
    "spec": {
      "objectives": [
        {"id": "request-count"},
        {"id": "error-rate"},
        {"id": "p95-latency"}
      ]
    }
  },
  "strategyId": "demo-app-canary-strategy",
  "strategyConfigRef": "configs/services/demo-app.strategy.yaml",
  "strategyConfigSnapshot": {
    "apiVersion": "delivery.ssentinel.io/v1alpha1",
    "kind": "ProgressiveDeliveryStrategy",
    "metadata": {
      "name": "demo-app-canary-strategy"
    },
    "spec": {
      "strategyType": "canary",
      "traffic": {
        "steps": [
          {"name": "initial-canary", "setWeight": 20, "pause": "30s"},
          {"name": "expand-canary", "setWeight": 50, "pause": "60s"},
          {"name": "full-promotion", "setWeight": 100, "pause": "0s"}
        ]
      },
      "failurePolicy": {
        "onSLOFailure": "stop_promotion",
        "onAnalysisError": "require_manual_review",
        "onInsufficientTraffic": "retry_with_more_traffic",
        "rollbackAllowed": false
      },
      "promotionPolicy": {
        "autoPromotionEnabled": false,
        "requiresHumanApproval": true,
        "finalPromotionMode": "manual"
      }
    }
  },
  "summary": {
    "rolloutPhase": "Degraded",
    "rolloutAbort": true,
    "analysisRunPhase": "Failed",
    "riskLevel": "critical",
    "riskScore": 100,
    "failedMetrics": ["error-rate", "p95-latency"],
    "matchedPolicyRules": ["multiple_slo_failure_requires_human_approval"]
  },
  "artifacts": {
    "releaseContext": "$TEST_TMP/release-context-test.json",
    "releaseReport": "$TEST_TMP/release-report-test.md",
    "aiAdvice": "$TEST_TMP/ai-advice-test.md",
    "aiDecision": "$TEST_TMP/ai-decision-test.json",
    "policyDecision": "$TEST_TMP/policy-decision-test.json",
    "releaseSummary": "$TEST_TMP/release-summary-test.md",
    "actionPlan": null,
    "actionPlanReport": null
  },
  "decisionRefs": {
    "aiDecision": {
      "decisionSource": "deterministic_rule",
      "confidence": "high"
    },
    "policyDecision": {
      "reason": "Multiple SLO gates failed"
    }
  }
}
JSON

touch \
  "$TEST_TMP/release-report-test.md" \
  "$TEST_TMP/ai-advice-test.md" \
  "$TEST_TMP/ai-decision-test.json" \
  "$TEST_TMP/policy-decision-test.json" \
  "$TEST_TMP/release-summary-test.md"

log "build evidence record"

EVIDENCE_RECORD_OUTPUT_DIR="$TEST_TMP" \
  scripts/build-evidence-record.sh "$TEST_TMP/release-evidence-20260521-100000.json"

RECORD="$TEST_TMP/evidence-record-20260521-100000.json"
LATEST="$TEST_TMP/evidence-record-latest.json"

[ -f "$RECORD" ] || fail "evidence record not generated"
[ -f "$LATEST" ] || fail "latest evidence record not generated"

log "assert evidence record content"

python3 - "$RECORD" "$LATEST" <<'PY'
import json
import sys
from pathlib import Path

record_path = Path(sys.argv[1])
latest_path = Path(sys.argv[2])

record = json.loads(record_path.read_text(encoding="utf-8"))
latest = json.loads(latest_path.read_text(encoding="utf-8"))

assert record["schemaVersion"] == "evidence.record/v1alpha1", record
assert record["releaseId"] == "20260521-100000", record
assert record["evidenceId"] == "ev-20260521-100000-demo-app-dev", record
assert record["service"] == "demo-app", record
assert record["namespace"] == "slo-rollout", record
assert record["env"] == "dev", record
assert record["slo"]["sloId"] == "demo-app-canary-slo", record
assert record["slo"]["snapshotCaptured"] is True, record
assert set(record["slo"]["objectiveIds"]) == {"request-count", "error-rate", "p95-latency"}, record
assert record["strategy"]["strategyId"] == "demo-app-canary-strategy", record
assert record["strategy"]["strategyConfigRef"] == "configs/services/demo-app.strategy.yaml", record
assert record["strategy"]["snapshotCaptured"] is True, record
assert record["strategy"]["strategyType"] == "canary", record
assert [step["setWeight"] for step in record["strategy"]["trafficSteps"]] == [20, 50, 100], record
assert record["strategy"]["failurePolicy"]["onSLOFailure"] == "stop_promotion", record
assert record["strategy"]["promotionPolicy"]["requiresHumanApproval"] is True, record
assert record["links"]["releaseEvidence"].endswith("release-evidence-20260521-100000.json"), record
assert record["artifacts"]["releaseContext"]["exists"] is True, record
assert record["coverage"]["total"] >= record["coverage"]["collected"], record
assert record["safety"]["readOnly"] is True, record
assert record["safety"]["willExecute"] is False, record
assert latest["evidenceId"] == record["evidenceId"], latest

print("PASS: evidence record content")
PY

log "validate evidence record contract"

python3 scripts/validate-release-contracts.py "$RECORD"

log "portal mapping compile check"

cat > watcher/portal_evidence_record_temp_test.go <<'GOEOF'
package main

import "testing"

func TestPortalEvidenceRecordResourceMapping(t *testing.T) {
	kind, contentType, ok := portalResourceKindFromPathSegment("evidence-record")
	if !ok {
		t.Fatal("expected evidence-record path segment to be supported")
	}
	if kind != "evidenceRecord" {
		t.Fatalf("expected kind evidenceRecord, got %q", kind)
	}
	if contentType != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", contentType)
	}

	if got := kindFromReportFile("evidence-record-20260521-100000.json"); got != "evidenceRecord" {
		t.Fatalf("expected evidenceRecord kind, got %q", got)
	}

	if got := releaseIDFromReportFile("evidence-record-20260521-100000.json"); got != "20260521-100000" {
		t.Fatalf("expected release id 20260521-100000, got %q", got)
	}

	def, ok := findPortalResourceDef("evidenceRecord")
	if !ok {
		t.Fatal("expected evidenceRecord resource definition")
	}
	if def.Endpoint != "/api/releases/latest/evidence-record" {
		t.Fatalf("unexpected endpoint: %q", def.Endpoint)
	}
}
GOEOF

(cd watcher && go test ./...)
rm -f watcher/portal_evidence_record_temp_test.go

log "final watcher compile check"
(cd watcher && go test ./...)

log "done"
echo "PASS: evidence record test passed"
