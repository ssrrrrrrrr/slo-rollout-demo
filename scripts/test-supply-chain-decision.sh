#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$BASE_DIR"

TEST_TMP="${1:-/tmp/slo-supply-chain-test}"
rm -rf "$TEST_TMP"
mkdir -p "$TEST_TMP"

echo
echo "===== run supply-chain safety decision test ====="

GOOD_CONTEXT="$TEST_TMP/release-context-good.json"
GOOD_DECISION="$TEST_TMP/supply-chain-decision-good.json"

cat > "$GOOD_CONTEXT" <<'JSON'
{
  "generatedAt": "2026-05-21T00:00:00Z",
  "namespace": "slo-rollout",
  "rollout": "demo-app",
  "rolloutPhase": "Healthy",
  "rolloutAbort": false,
  "currentDesiredVersion": "v11-actions",
  "analysisRun": "demo-app-pass-analysis",
  "analysisRunPhase": "Successful",
  "failedMetrics": [],
  "analysisRunMetrics": [],
  "severity": "low",
  "riskScore": 0,
  "riskReasons": [],
  "service": "demo-app",
  "env": "dev",
  "changeContext": {
    "commit": "abc1234",
    "image": {
      "current": "registry.local:5000/sre/demo-app:v11-actions",
      "digest": "sha256:1111222233334444"
    }
  }
}
JSON

SUPPLY_CHAIN_DECISION_OUTPUT_FILE="$GOOD_DECISION" \
GITOPS_ROLLOUT_FILE="deploy/base/rollout.yaml" \
  ./scripts/build-supply-chain-decision.sh "$GOOD_CONTEXT" \
  >"$TEST_TMP/good.log" 2>&1

cat "$TEST_TMP/good.log"

[ -f "$GOOD_DECISION" ] || { echo "FAILED: good supply-chain decision not generated" >&2; exit 1; }

python3 scripts/validate-release-contracts.py "$GOOD_DECISION"

python3 - "$GOOD_DECISION" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))

assert doc["schemaVersion"] == "supply.chain.decision/v1alpha1", doc
assert doc["mode"] == "read_only_supply_chain_check", doc
assert doc["release"]["service"] == "demo-app", doc["release"]
assert doc["release"]["env"] == "dev", doc["release"]
assert doc["release"]["version"] == "v11-actions", doc["release"]
assert doc["release"]["commit"] == "abc1234", doc["release"]
assert doc["image"]["imageTag"] == "v11-actions", doc["image"]
assert doc["image"]["imageDigest"] == "sha256:1111222233334444", doc["image"]
assert doc["gitops"]["manifestFound"] is True, doc["gitops"]
assert doc["gitops"]["releaseTag"] == "v11-actions", doc["gitops"]
assert doc["decision"]["decision"] == "ALLOW", doc["decision"]
assert doc["decision"]["requiresHumanApproval"] is False, doc["decision"]
assert doc["decision"]["allowed"] is True, doc["decision"]
assert doc["guardrails"]["readOnly"] is True, doc["guardrails"]
assert doc["guardrails"]["willExecute"] is False, doc["guardrails"]
assert doc["guardrails"]["doesNotModifyKubernetes"] is True, doc["guardrails"]
assert doc["guardrails"]["doesNotModifyGitOps"] is True, doc["guardrails"]
assert doc["guardrails"]["doesNotBuildImages"] is True, doc["guardrails"]
assert doc["guardrails"]["doesNotPushImages"] is True, doc["guardrails"]
assert doc["guardrails"]["doesNotCommitOrPush"] is True, doc["guardrails"]

checks = {item["checkId"]: item for item in doc["checks"]}
assert checks["gitops_version_matches_release"]["status"] == "PASS", checks["gitops_version_matches_release"]
assert checks["image_tag_matches_release_version"]["status"] == "PASS", checks["image_tag_matches_release_version"]

print("PASS: healthy supply-chain decision content")
PY

RISKY_CONTEXT="$TEST_TMP/release-context-risky.json"
RISKY_EVIDENCE="$TEST_TMP/release-evidence-risky.json"
RISKY_DECISION="$TEST_TMP/supply-chain-decision-risky.json"

cat > "$RISKY_CONTEXT" <<'JSON'
{
  "generatedAt": "2026-05-21T00:00:00Z",
  "namespace": "slo-rollout",
  "rollout": "demo-app",
  "rolloutPhase": "Healthy",
  "rolloutAbort": false,
  "currentDesiredVersion": "latest",
  "analysisRun": "demo-app-pass-analysis",
  "analysisRunPhase": "Successful",
  "failedMetrics": [],
  "analysisRunMetrics": [],
  "severity": "medium",
  "riskScore": 20,
  "riskReasons": [],
  "service": "demo-app",
  "env": "dev",
  "changeContext": {
    "image": {
      "current": "registry.local:5000/sre/demo-app:latest"
    }
  }
}
JSON

cat > "$RISKY_EVIDENCE" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test",
  "releaseResult": "PASS",
  "policyDecision": "ALLOW_ADVISORY_ONLY",
  "finalAction": "NOOP",
  "executionMode": "advisory_only",
  "requiresHumanApproval": false,
  "safeToRetry": true,
  "summary": {
    "rolloutPhase": "Healthy",
    "rolloutAbort": false,
    "analysisRunPhase": "Successful",
    "riskLevel": "low",
    "riskScore": 0,
    "failedMetrics": [],
    "matchedPolicyRules": []
  },
  "artifacts": {
    "releaseContext": "$RISKY_CONTEXT",
    "aiDecision": null,
    "policyDecision": null,
    "releaseSummary": null,
    "actionPlan": null
  },
  "decisionRefs": {}
}
JSON

SUPPLY_CHAIN_DECISION_OUTPUT_FILE="$RISKY_DECISION" \
GITOPS_ROLLOUT_FILE="$TEST_TMP/not-found-rollout.yaml" \
  ./scripts/build-supply-chain-decision.sh "$RISKY_EVIDENCE" \
  >"$TEST_TMP/risky.log" 2>&1

cat "$TEST_TMP/risky.log"

[ -f "$RISKY_DECISION" ] || { echo "FAILED: risky supply-chain decision not generated" >&2; exit 1; }

python3 scripts/validate-release-contracts.py "$RISKY_DECISION"

python3 - "$RISKY_DECISION" "$RISKY_EVIDENCE" "$RISKY_CONTEXT" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
evidence_path = Path(sys.argv[2])
context_path = Path(sys.argv[3])

assert doc["schemaVersion"] == "supply.chain.decision/v1alpha1", doc
assert doc["source"]["inputKind"] == "release_evidence", doc["source"]
assert doc["source"]["releaseEvidence"] == str(evidence_path), doc["source"]
assert doc["source"]["releaseContext"] == str(context_path), doc["source"]
assert doc["release"]["version"] == "latest", doc["release"]
assert doc["release"]["commit"] is None, doc["release"]
assert doc["image"]["imageTag"] == "latest", doc["image"]
assert doc["image"]["usesMutableTag"] is True, doc["image"]
assert doc["gitops"]["manifestFound"] is False, doc["gitops"]
assert doc["decision"]["decision"] == "REQUIRE_HUMAN_APPROVAL", doc["decision"]
assert doc["decision"]["requiresHumanApproval"] is True, doc["decision"]
assert doc["decision"]["allowed"] is True, doc["decision"]
assert doc["risk"]["riskScore"] >= 40, doc["risk"]
assert doc["guardrails"]["willExecute"] is False, doc["guardrails"]

checks = {item["checkId"]: item for item in doc["checks"]}
assert checks["mutable_image_tag"]["status"] == "WARN", checks["mutable_image_tag"]
assert checks["source_commit_present"]["status"] == "WARN", checks["source_commit_present"]
assert checks["image_digest_present"]["status"] == "WARN", checks["image_digest_present"]

print("PASS: risky supply-chain decision content")
PY

echo "PASS: supply-chain decision test passed"
