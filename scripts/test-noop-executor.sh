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

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-noop-executor-test}"
REPORT_DIR="$TMP_DIR/reports"
BUILD_DIR="$TMP_DIR/build/compiled/dev"

rm -rf "$TMP_DIR"
mkdir -p "$REPORT_DIR" "$BUILD_DIR"

RELEASE_ID="20260101-040404"

cat > "$REPORT_DIR/release-evidence-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-noop-executor.sh",
  "releaseId": "$RELEASE_ID",
  "generatedAt": "2026-01-01T04:04:04Z",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "executionMode": "manual_approval",
  "requiresHumanApproval": true,
  "safeToRetry": false,
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "summary": {
    "riskLevel": "high",
    "riskScore": 85,
    "rolloutPhase": "Paused",
    "rolloutAbort": false,
    "analysisRunPhase": "Running",
    "matchedPolicyRules": ["signed_release_gate_requires_human_approval"],
    "failedMetrics": ["error-rate"]
  },
  "artifacts": {
    "releaseContext": "release-context-$RELEASE_ID.json",
    "aiDecision": "ai-decision-$RELEASE_ID.json",
    "policyDecision": "policy-decision-$RELEASE_ID.json",
    "releaseSummary": "release-summary-$RELEASE_ID.md",
    "executionRequest": "execution-request-$RELEASE_ID.json",
    "executionEligibility": "execution-eligibility-$RELEASE_ID.json",
    "actionPlan": "action-plan-$RELEASE_ID.json",
    "supplyChainDecision": "supply-chain-decision-$RELEASE_ID.json"
  },
  "decisionRefs": {}
}
JSON

cat > "$REPORT_DIR/release-context-$RELEASE_ID.json" <<JSON
{"schemaVersion":"release.context/v1alpha1","releaseId":"$RELEASE_ID","service":"demo-app","env":"dev","namespace":"slo-rollout"}
JSON

cat > "$REPORT_DIR/ai-decision-$RELEASE_ID.json" <<JSON
{"schemaVersion":"release.ai.decision/v1alpha1","decision":"STOP_PROMOTION"}
JSON

cat > "$REPORT_DIR/policy-decision-$RELEASE_ID.json" <<JSON
{"schemaVersion":"release.policy.evaluator/v1alpha1","policyDecision":"REQUIRE_HUMAN_APPROVAL","finalAction":"STOP_PROMOTION","requestedAction":"STOP_PROMOTION","allowed":true,"requiresHumanApproval":true}
JSON

cat > "$REPORT_DIR/release-summary-$RELEASE_ID.md" <<'MD'
# Release Summary

Noop executor fixture.
MD

cat > "$REPORT_DIR/execution-request-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "execution.request/v1alpha1",
  "executionRequestId": "er-$RELEASE_ID",
  "generatedBy": "test-noop-executor.sh",
  "generatedAt": "2026-01-01T04:04:05Z",
  "mode": "request_only",
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
    "requestStatus": "WAITING_FOR_APPROVAL",
    "lifecycleStage": "WAITING_APPROVAL",
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

cat > "$REPORT_DIR/execution-eligibility-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "execution.eligibility/v1alpha1",
  "eligibilityDecisionId": "el-$RELEASE_ID",
  "generatedBy": "test-noop-executor.sh",
  "generatedAt": "2026-01-01T04:04:06Z",
  "mode": "read_only_eligibility_assessment",
  "release": {
    "releaseId": "$RELEASE_ID",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "policyDecision": "REQUIRE_HUMAN_APPROVAL",
    "finalAction": "STOP_PROMOTION"
  },
  "executionRequest": {
    "executionRequestId": "er-$RELEASE_ID",
    "requestedAction": "STOP_PROMOTION",
    "requestStatus": "WAITING_FOR_APPROVAL",
    "lifecycleStage": "WAITING_APPROVAL"
  },
  "approval": {
    "required": true,
    "status": "PENDING",
    "approvalDecision": null,
    "approved": false,
    "readyToExecute": false
  },
  "supplyChain": {
    "decision": "REQUIRE_HUMAN_APPROVAL"
  },
  "signedReleaseGate": {},
  "decision": {
    "finalStatus": "WAITING_APPROVAL",
    "readyToExecute": false,
    "blockingReasons": [],
    "approvalReasons": ["human_approval_required"],
    "missingInputs": [],
    "summary": "Execution request for STOP_PROMOTION is waiting for human approval."
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$REPORT_DIR/action-plan-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "release.action-plan/v1alpha1",
  "generatedBy": "test-noop-executor.sh",
  "generatedAt": "2026-01-01T04:04:07Z",
  "sourceReleaseEvidence": "release-evidence-$RELEASE_ID.json",
  "releaseResult": "FAIL_BY_MULTIPLE_SLO",
  "policyDecision": "REQUIRE_HUMAN_APPROVAL",
  "finalAction": "STOP_PROMOTION",
  "executionMode": "dry_run",
  "sourceExecutionMode": "manual_approval",
  "willExecute": false,
  "requiresHumanApproval": true,
  "target": {
    "namespace": "slo-rollout",
    "rollout": "demo-app",
    "analysisRun": "demo-app-analysis"
  },
  "actionPlan": {
    "action": "STOP_PROMOTION",
    "blocked": false,
    "blockReason": null,
    "candidateCommands": [
      {
        "name": "inspect_rollout",
        "command": "kubectl argo rollouts get rollout demo-app -n slo-rollout",
        "type": "read_only",
        "willExecute": false
      }
    ],
    "humanSteps": [
      "Stop promotion and inspect rollout health."
    ]
  },
  "guardrails": {
    "advisoryOnly": true,
    "dryRunOnly": true,
    "doesNotModifyGitOps": true,
    "doesNotModifyKubernetes": true,
    "doesNotRollback": true,
    "doesNotPromote": true,
    "doesNotPatchResources": true,
    "doesNotDeleteResources": true,
    "doesNotBuildImages": true,
    "doesNotCommitOrPush": true
  }
}
JSON

cat > "$REPORT_DIR/supply-chain-decision-$RELEASE_ID.json" <<JSON
{
  "schemaVersion": "supply.chain.decision/v1alpha1",
  "supplyChainDecisionId": "sc-$RELEASE_ID",
  "generatedBy": "test-noop-executor.sh",
  "generatedAt": "2026-01-01T04:04:08Z",
  "decision": {
    "decision": "REQUIRE_HUMAN_APPROVAL",
    "requiresHumanApproval": true,
    "allowed": true,
    "warningReasons": ["mutable image tag requires approval"]
  }
}
JSON

cat > "$BUILD_DIR/rendered-release-plan.json" <<JSON
{
  "schemaVersion": "ssentinel.rendered-release-plan/v1alpha1",
  "kind": "RenderedReleasePlan",
  "generatedAt": "2026-01-01T04:04:09Z",
  "generatedBy": "scripts/compile-release-config.sh",
  "release": {
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "appVersion": "v1"
  },
  "inputs": {
    "overlayPath": "deploy/overlays/dev"
  },
  "outputs": {
    "outputDir": "build/compiled/dev",
    "analysisTemplate": "analysis.yaml",
    "rollout": "rollout.yaml",
    "kustomization": "kustomization.yaml",
    "artifacts": [
      {"kind": "Rollout", "path": "rollout.yaml", "rendererRef": "argo-rollouts-canary-v1"}
    ]
  },
  "strategy": {
    "strategyId": "demo-app-canary",
    "strategyType": "canary",
    "trafficSteps": [
      {"name": "canary-10", "setWeight": 10, "pause": "60s"}
    ]
  }
}
JSON

echo "===== run noop executor ====="
EXECUTION_PREVIEW_RENDERED_PLAN="$BUILD_DIR/rendered-release-plan.json" \
  ./scripts/run-noop-executor.sh "$REPORT_DIR/release-evidence-$RELEASE_ID.json" > "$TMP_DIR/noop.log"
cat "$TMP_DIR/noop.log"

echo
echo "===== assert noop executor ====="
"$PYTHON_BIN" - "$REPORT_DIR/release-evidence-$RELEASE_ID.json" "$REPORT_DIR/execution-result-$RELEASE_ID.json" "$REPORT_DIR/evidence-record-$RELEASE_ID.json" <<'PY'
import json
import sys
from pathlib import Path

evidence = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
result = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
record = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))

assert result["schemaVersion"] == "execution.result/v1alpha1"
assert result["executionResultId"] == "xr-20260101-040404"
assert result["result"]["executionStatus"] == "PREVIEW_ONLY"
assert evidence["decisionRefs"]["executionResult"]["executionStatus"] == "PREVIEW_ONLY"
assert record["executionResult"]["executionResultId"] == "xr-20260101-040404"
assert record["executionResult"]["executionStatus"] == "PREVIEW_ONLY"

print("PASS: noop executor generated execution result and evidence record")
PY
echo
echo "===== validate contracts ====="
"$PYTHON_BIN" ./scripts/validate-release-contracts.py \
  "$REPORT_DIR/release-evidence-$RELEASE_ID.json" \
  "$REPORT_DIR/execution-preview-$RELEASE_ID.json" \
  "$REPORT_DIR/execution-result-$RELEASE_ID.json" \
  "$REPORT_DIR/evidence-record-$RELEASE_ID.json"

echo
echo "PASS: noop executor test passed"
