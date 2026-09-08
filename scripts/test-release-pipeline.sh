#!/usr/bin/env bash
set -euo pipefail

REPORT_FIXTURE="tests/fixtures/release-report/minimal-report.md"
PASS_CONTEXT="tests/fixtures/release-context/pass.json"
P95_CONTEXT="tests/fixtures/release-context/fail-p95-latency.json"
MULTIPLE_CONTEXT="tests/fixtures/release-context/fail-multiple-slo.json"
CRITICAL_CHANGE_CONTEXT="tests/fixtures/change-context/critical-risk.json"

TEST_TMP="${TEST_TMP:-/tmp/slo-release-pipeline-test}"
rm -rf "$TEST_TMP"
mkdir -p "$TEST_TMP"

log() {
  echo
  echo "===== $* ====="
}

fail() {
  echo "FAILED: $*" >&2
  exit 1
}

latest_file() {
  local pattern="$1"

  python3 - "$pattern" <<'LATEST_FILE_PY'
import glob
import os
import sys

pattern = sys.argv[1]
files = [f for f in glob.glob(pattern) if os.path.isfile(f)]
files.sort(key=lambda f: os.path.getmtime(f), reverse=True)

if files:
    print(files[0])
LATEST_FILE_PY
}

run_advisor_case() {
  local name="$1"
  local context_file="$2"

  log "run advisor case: $name"

  RELEASE_CONTEXT_FILE="$context_file" \
  MODEL=qwen2.5:0.5b \
  OLLAMA_URL=http://127.0.0.1:9 \
  OLLAMA_TIMEOUT_SECONDS=2 \
  OLLAMA_NUM_CTX=256 \
  OLLAMA_NUM_PREDICT=64 \
  ADVISOR_REPORT_TEXT_LIMIT=1000 \
  FAILURE_EVIDENCE_OUTPUT_DIR="$TEST_TMP/${name}-failure-evidence" \
  ./scripts/ai-release-advisor.sh "$REPORT_FIXTURE" >"$TEST_TMP/${name}-advisor.log" 2>&1

  cat "$TEST_TMP/${name}-advisor.log"

  local ai_decision
  local policy_decision
  local release_evidence
  local release_summary

  ai_decision="$(latest_file 'docs/release-reports/ai-decision-*.json')"
  policy_decision="$(latest_file 'docs/release-reports/policy-decision-*.json')"
  release_evidence="$(latest_file 'docs/release-reports/release-evidence-*.json')"
  release_summary="$(latest_file 'docs/release-reports/release-summary-*.md')"

  [ -f "$ai_decision" ] || fail "$name ai decision not generated"
  [ -f "$policy_decision" ] || fail "$name policy decision not generated"
  [ -f "$release_evidence" ] || fail "$name release evidence not generated"
  [ -f "$release_summary" ] || fail "$name release summary not generated"

  echo "$ai_decision" >"$TEST_TMP/${name}.ai"
  echo "$policy_decision" >"$TEST_TMP/${name}.policy"
  echo "$release_evidence" >"$TEST_TMP/${name}.evidence"
  echo "$release_summary" >"$TEST_TMP/${name}.summary"

  local failure_evidence_json="$TEST_TMP/${name}-failure-evidence/failure-evidence-latest.json"
  local failure_evidence_md="$TEST_TMP/${name}-failure-evidence/failure-evidence-latest.md"

  if [ -f "$failure_evidence_json" ]; then
    echo "$failure_evidence_json" >"$TEST_TMP/${name}.failure.json"
  else
    echo "" >"$TEST_TMP/${name}.failure.json"
  fi

  if [ -f "$failure_evidence_md" ]; then
    echo "$failure_evidence_md" >"$TEST_TMP/${name}.failure.md"
  else
    echo "" >"$TEST_TMP/${name}.failure.md"
  fi
}

assert_pass_case() {
  local ai policy evidence summary
  ai="$(cat "$TEST_TMP/pass.ai")"
  policy="$(cat "$TEST_TMP/pass.policy")"
  evidence="$(cat "$TEST_TMP/pass.evidence")"
  summary="$(cat "$TEST_TMP/pass.summary")"

  python3 - "$ai" "$policy" "$evidence" "$summary" <<'PY'
import json
import sys
from pathlib import Path

ai = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
policy = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
evidence = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
summary_path = Path(sys.argv[4])

assert ai["releaseResult"] == "PASS", ai["releaseResult"]
assert ai["agentAction"]["type"] == "NOOP", ai["agentAction"]
assert ai["requiresHumanApproval"] is False
assert ai["safeToRetry"] is True

assert policy["policyDecision"] == "ALLOW_ADVISORY_ONLY", policy
assert policy["requestedAction"] == "NOOP", policy
assert policy["allowed"] is True, policy
assert policy["finalAction"] == "NOOP", policy
assert policy["requiresHumanApproval"] is False
assert policy["safety"]["readOnly"] is True, policy
assert policy["safety"]["willExecute"] is False, policy
assert "pass_release_no_action" in policy["matchedRules"], policy["matchedRules"]

assert evidence["releaseResult"] == "PASS", evidence
assert evidence["policyDecision"] == "ALLOW_ADVISORY_ONLY", evidence
assert evidence["requestedAction"] == "NOOP", evidence
assert evidence["allowed"] is True, evidence
assert evidence["policySafety"]["willExecute"] is False, evidence
assert evidence["decisionRefs"]["policyDecision"]["requestedAction"] == "NOOP", evidence["decisionRefs"]["policyDecision"]
assert evidence["decisionRefs"]["policyDecision"]["allowed"] is True, evidence["decisionRefs"]["policyDecision"]
assert evidence["artifacts"].get("releaseSummary"), evidence["artifacts"]
assert summary_path.exists(), summary_path

text = summary_path.read_text(encoding="utf-8")
assert "发布摘要" in text
assert "发布结果：`PASS`" in text

print("PASS release pipeline test passed")
PY
}

assert_p95_case() {
  local ai policy evidence failure_json failure_md
  ai="$(cat "$TEST_TMP/p95.ai")"
  policy="$(cat "$TEST_TMP/p95.policy")"
  evidence="$(cat "$TEST_TMP/p95.evidence")"
  failure_json="$(cat "$TEST_TMP/p95.failure.json")"
  failure_md="$(cat "$TEST_TMP/p95.failure.md")"

  [ -f "$failure_json" ] || fail "p95 failure evidence json not generated"
  [ -f "$failure_md" ] || fail "p95 failure evidence md not generated"

  python3 - "$ai" "$policy" "$evidence" "$failure_json" "$failure_md" <<'PY'
import json
import sys
from pathlib import Path

ai = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
policy = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
evidence = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
failure = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))
failure_md = Path(sys.argv[5]).read_text(encoding="utf-8")

assert ai["releaseResult"] == "FAIL_BY_P95_LATENCY", ai["releaseResult"]
assert ai["agentAction"]["type"] == "STOP_PROMOTION", ai["agentAction"]
assert ai["requiresHumanApproval"] is True
assert ai["safeToRetry"] is False

assert policy["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", policy
assert policy["requestedAction"] == "STOP_PROMOTION", policy
assert policy["allowed"] is True, policy
assert policy["finalAction"] == "STOP_PROMOTION", policy
assert policy["requiresHumanApproval"] is True
assert policy["safety"]["readOnly"] is True, policy
assert policy["safety"]["willExecute"] is False, policy
assert "slo_failure_stop_promotion_allowed_by_strategy" in policy["matchedRules"], policy["matchedRules"]

assert evidence["releaseResult"] == "FAIL_BY_P95_LATENCY", evidence
assert evidence["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", evidence
assert evidence["requestedAction"] == "STOP_PROMOTION", evidence
assert evidence["allowed"] is True, evidence
assert evidence["finalAction"] == "STOP_PROMOTION", evidence
assert evidence["decisionRefs"]["policyDecision"]["requestedAction"] == "STOP_PROMOTION", evidence["decisionRefs"]["policyDecision"]
assert evidence["decisionRefs"]["policyDecision"]["allowed"] is True, evidence["decisionRefs"]["policyDecision"]
assert "p95-latency" in evidence["summary"]["failedMetrics"], evidence["summary"]

assert failure["isFailure"] is True, failure
assert failure["executionMode"] == "advisory_only", failure
assert "p95-latency" in failure["release"]["failedMetrics"], failure["release"]
assert failure["guardrails"]["doesNotRollback"] is True, failure["guardrails"]
assert "故障诊断证据" in failure_md

assert evidence["artifacts"].get("failureEvidence"), evidence["artifacts"]
assert evidence["artifacts"].get("failureEvidenceReport"), evidence["artifacts"]
assert Path(evidence["artifacts"]["failureEvidence"]).exists(), evidence["artifacts"]
assert Path(evidence["artifacts"]["failureEvidenceReport"]).exists(), evidence["artifacts"]

print("P95 failure policy test passed")
PY
}

assert_multiple_slo_case() {
  local ai policy evidence failure_json failure_md
  ai="$(cat "$TEST_TMP/multiple.ai")"
  policy="$(cat "$TEST_TMP/multiple.policy")"
  evidence="$(cat "$TEST_TMP/multiple.evidence")"
  failure_json="$(cat "$TEST_TMP/multiple.failure.json")"
  failure_md="$(cat "$TEST_TMP/multiple.failure.md")"

  [ -f "$failure_json" ] || fail "multiple failure evidence json not generated"
  [ -f "$failure_md" ] || fail "multiple failure evidence md not generated"

  python3 - "$ai" "$policy" "$evidence" "$failure_json" "$failure_md" <<'PY'
import json
import sys
from pathlib import Path

ai = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
policy = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
evidence = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))
failure = json.loads(Path(sys.argv[4]).read_text(encoding="utf-8"))
failure_md = Path(sys.argv[5]).read_text(encoding="utf-8")

assert ai["releaseResult"] == "FAIL_BY_MULTIPLE_SLO", ai["releaseResult"]
assert ai["agentAction"]["type"] == "STOP_PROMOTION", ai["agentAction"]
assert ai["requiresHumanApproval"] is True
assert ai["safeToRetry"] is False

assert policy["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", policy
assert policy["requestedAction"] == "STOP_PROMOTION", policy
assert policy["allowed"] is True, policy
assert policy["finalAction"] == "STOP_PROMOTION", policy
assert policy["requiresHumanApproval"] is True
assert policy["safety"]["readOnly"] is True, policy
assert policy["safety"]["willExecute"] is False, policy
assert "slo_failure_stop_promotion_allowed_by_strategy" in policy["matchedRules"], policy["matchedRules"]

assert evidence["releaseResult"] == "FAIL_BY_MULTIPLE_SLO", evidence
assert evidence["policyDecision"] == "REQUIRE_HUMAN_APPROVAL", evidence
assert evidence["requestedAction"] == "STOP_PROMOTION", evidence
assert evidence["allowed"] is True, evidence
assert evidence["decisionRefs"]["policyDecision"]["requestedAction"] == "STOP_PROMOTION", evidence["decisionRefs"]["policyDecision"]
assert evidence["decisionRefs"]["policyDecision"]["allowed"] is True, evidence["decisionRefs"]["policyDecision"]
assert set(evidence["summary"]["failedMetrics"]) == {"error-rate", "p95-latency"}, evidence["summary"]

assert failure["isFailure"] is True, failure
assert failure["executionMode"] == "advisory_only", failure
assert set(failure["release"]["failedMetrics"]) == {"error-rate", "p95-latency"}, failure["release"]
assert failure["guardrails"]["doesNotRollback"] is True, failure["guardrails"]
assert "多 SLO 失败" in failure_md

assert evidence["artifacts"].get("failureEvidence"), evidence["artifacts"]
assert evidence["artifacts"].get("failureEvidenceReport"), evidence["artifacts"]
assert Path(evidence["artifacts"]["failureEvidence"]).exists(), evidence["artifacts"]
assert Path(evidence["artifacts"]["failureEvidenceReport"]).exists(), evidence["artifacts"]

print("Multiple SLO failure policy test passed")
PY
}

assert_change_risk_case() {
  log "run critical change risk test"

  local tmp_change_dir="$TEST_TMP/change-risk"
  mkdir -p "$tmp_change_dir"
  cp "$CRITICAL_CHANGE_CONTEXT" "$tmp_change_dir/change-context-critical.json"

  ./scripts/evaluate-change-risk.sh "$tmp_change_dir/change-context-critical.json" >"$TEST_TMP/change-risk.log" 2>&1
  cat "$TEST_TMP/change-risk.log"

  local decision="$tmp_change_dir/change-risk-decision-latest.json"
  [ -f "$decision" ] || fail "change-risk decision not generated"

  python3 - "$decision" <<'PY'
import json
import sys
from pathlib import Path

decision = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))

assert decision["riskDecision"] == "RECOMMEND_BLOCK", decision
assert decision["riskLevel"] == "critical", decision
assert decision["riskScore"] == 100, decision
assert decision["requiresHumanApproval"] is True, decision
assert decision["recommendedAction"] == "manual_review_before_canary", decision
assert decision["guardrails"]["autoBlock"] is False, decision["guardrails"]
assert decision["guardrails"]["doesNotModifyKubernetes"] is True, decision["guardrails"]

print("Critical change risk test passed")
PY
}

assert_failure_evidence_case() {
  log "run failure evidence test"

  local tmp_failure_dir="$TEST_TMP/failure"
  mkdir -p "$tmp_failure_dir"
  cp "$MULTIPLE_CONTEXT" "$tmp_failure_dir/release-context-multiple.json"

  ./scripts/collect-failure-evidence.sh "$tmp_failure_dir/release-context-multiple.json" >"$TEST_TMP/failure-evidence.log" 2>&1
  cat "$TEST_TMP/failure-evidence.log"

  local failure_json="$tmp_failure_dir/failure-evidence-latest.json"
  local failure_md="$tmp_failure_dir/failure-evidence-latest.md"

  [ -f "$failure_json" ] || fail "failure evidence json not generated"
  [ -f "$failure_md" ] || fail "failure evidence md not generated"

  python3 - "$failure_json" "$failure_md" <<'PY'
import json
import sys
from pathlib import Path

failure = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
md = Path(sys.argv[2]).read_text(encoding="utf-8")

assert failure["schemaVersion"] == "failure.evidence/v1alpha1", failure
assert failure["executionMode"] == "advisory_only", failure
assert failure["isFailure"] is True, failure
assert set(failure["release"]["failedMetrics"]) == {"error-rate", "p95-latency"}, failure["release"]
assert failure["guardrails"]["doesNotRollback"] is True, failure["guardrails"]
assert failure["guardrails"]["doesNotDeleteResources"] is True, failure["guardrails"]

assert "故障诊断证据" in md
assert "多 SLO 失败" in md
assert "advisory_only" in md

print("Failure evidence test passed")
PY
}

main() {
  log "syntax checks"
  bash -n scripts/ai-release-advisor.sh
  bash -n scripts/evaluate-agent-decision.sh
  bash -n scripts/build-release-evidence.sh
  bash -n scripts/build-release-summary.sh
  bash -n scripts/evaluate-change-risk.sh
  bash -n scripts/collect-failure-evidence.sh
  bash -n scripts/agent-tool-router.sh
  bash -n scripts/build-action-plan.sh
  bash -n scripts/test-advisor-action-plan.sh
  bash -n scripts/build-release-memory.sh
  bash -n scripts/query-release-memory.sh
  bash -n scripts/test-advisor-release-memory.sh
  bash -n scripts/build-release-intelligence.sh
  bash -n scripts/build-agent-run.sh
  bash -n scripts/build-plan-run.sh
  bash -n scripts/build-execution-request.sh
  bash -n scripts/build-supply-chain-decision.sh
  bash -n scripts/build-execution-preview.sh
  bash -n scripts/build-execution-result.sh
  bash -n scripts/build-gitops-patch-proposal.sh
  bash -n scripts/build-gitops-pr-bundle.sh
  bash -n scripts/build-gitops-handoff-bundle.sh
  bash -n scripts/build-gitops-adapter-request.sh
  bash -n scripts/build-gitops-adapter-result.sh
  bash -n scripts/build-gitops-adapter-delivery.sh
  bash -n scripts/build-gitops-adapter-run.sh
  bash -n scripts/build-gitops-adapter-pickup.sh
  bash -n scripts/build-gitops-adapter-pickup-ack.sh
  bash -n scripts/build-gitops-adapter-handoff-state.sh
  bash -n scripts/run-noop-executor.sh
  bash -n scripts/test-release-intelligence.sh
  bash -n scripts/test-readonly-release-agent.sh
  bash -n scripts/test-agent-planning.sh
  bash -n scripts/test-execution-request.sh
  bash -n scripts/test-execution-preview.sh
  bash -n scripts/test-execution-result.sh
  bash -n scripts/test-gitops-patch-proposal.sh
  bash -n scripts/test-gitops-pr-bundle.sh
  bash -n scripts/test-gitops-handoff-bundle.sh
  bash -n scripts/test-gitops-adapter-request.sh
  bash -n scripts/test-gitops-adapter-result.sh
  bash -n scripts/test-gitops-adapter-delivery.sh
  bash -n scripts/test-gitops-adapter-run.sh
  bash -n scripts/test-gitops-adapter-pickup.sh
  bash -n scripts/test-gitops-adapter-pickup-ack.sh
  bash -n scripts/test-gitops-adapter-handoff-state.sh
  bash -n scripts/test-noop-executor.sh
  bash -n scripts/test-supply-chain-decision.sh
  bash -n scripts/test-agent-tool-router-intelligence.sh
  bash -n scripts/test-advisor-release-intelligence.sh
  bash -n scripts/test-release-summary-intelligence.sh
  bash -n scripts/test-ai-advice-intelligence.sh
  bash -n scripts/create-approval-record.sh
  bash -n scripts/test-approval-record.sh
  bash -n scripts/test-agent-tool-router-approval.sh
  bash -n scripts/test-approval-record-evidence-link.sh
  bash -n scripts/test-approval-record-report-integration.sh
  bash -n scripts/test-environment-config.sh
  bash -n scripts/test-packaging-boundary.sh
  bash -n scripts/test-evidence-environment-integration.sh
  bash -n scripts/test-environment-selection.sh

  sleep 1
  run_advisor_case "pass" "$PASS_CONTEXT"
  assert_pass_case

  sleep 1
  run_advisor_case "p95" "$P95_CONTEXT"
  assert_p95_case

  sleep 1
  run_advisor_case "multiple" "$MULTIPLE_CONTEXT"
  assert_multiple_slo_case

  assert_change_risk_case
  assert_failure_evidence_case
  ./scripts/test-advisor-action-plan.sh "$TEST_TMP/advisor-action-plan"
  ./scripts/test-advisor-release-memory.sh "$TEST_TMP/advisor-release-memory"
  ./scripts/test-release-intelligence.sh "$TEST_TMP/release-intelligence"
  ./scripts/test-agent-tool-router-intelligence.sh "$TEST_TMP/router-intelligence"
  ./scripts/test-advisor-release-intelligence.sh "$TEST_TMP/advisor-release-intelligence"
  ./scripts/test-readonly-release-agent.sh "$TEST_TMP/readonly-release-agent"
  ./scripts/test-agent-planning.sh "$TEST_TMP/agent-planning"
  ./scripts/test-execution-request.sh "$TEST_TMP/execution-request"
  ./scripts/test-execution-preview.sh
  ./scripts/test-execution-result.sh
  ./scripts/test-gitops-patch-proposal.sh
  ./scripts/test-gitops-pr-bundle.sh
  ./scripts/test-gitops-handoff-bundle.sh
  ./scripts/test-gitops-adapter-request.sh
  ./scripts/test-gitops-adapter-result.sh
  ./scripts/test-gitops-adapter-delivery.sh
  ./scripts/test-gitops-adapter-run.sh
  ./scripts/test-gitops-adapter-pickup.sh
  ./scripts/test-gitops-adapter-pickup-ack.sh
  ./scripts/test-gitops-adapter-handoff-state.sh
  ./scripts/test-noop-executor.sh
  ./scripts/test-supply-chain-decision.sh "$TEST_TMP/supply-chain-decision"
  ./scripts/test-release-summary-intelligence.sh "$TEST_TMP/release-summary-intelligence"
  ./scripts/test-ai-advice-intelligence.sh "$TEST_TMP/ai-advice-intelligence"
  ./scripts/test-approval-record.sh "$TEST_TMP/approval-record"
  ./scripts/test-agent-tool-router-approval.sh "$TEST_TMP/router-approval"
  ./scripts/test-approval-record-evidence-link.sh "$TEST_TMP/approval-evidence-link"
  ./scripts/test-approval-record-report-integration.sh "$TEST_TMP/approval-report-integration"
  ./scripts/test-environment-config.sh
  ./scripts/test-packaging-boundary.sh
  ./scripts/test-evidence-environment-integration.sh
  ./scripts/test-environment-selection.sh "$TEST_TMP/environment-selection"
  log "ALL OFFLINE RELEASE PIPELINE TESTS PASSED"
}

main "$@"
