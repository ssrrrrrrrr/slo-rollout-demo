#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PYTHON_BIN="${PYTHON_BIN:-python3}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

SUPPLY_CHAIN="$TMP_DIR/supply-chain-decision-20260101-000000.json"
PASS_GATE="$TMP_DIR/signed-release-gate-pass.json"
FAIL_GATE="$TMP_DIR/signed-release-gate-fail.json"
FAKE_PASS="$TMP_DIR/fake-cosign-pass"
FAKE_FAIL="$TMP_DIR/fake-cosign-fail"

cat > "$SUPPLY_CHAIN" <<'JSON'
{
  "schemaVersion": "supply.chain.decision/v1alpha1",
  "supplyChainDecisionId": "sc-20260101-000000",
  "generatedBy": "test-verification-gate-decision.sh",
  "generatedAt": "2026-01-01T00:00:00Z",
  "mode": "read_only_supply_chain_check",
  "release": {
    "releaseId": "20260101-000000",
    "service": "demo-app",
    "env": "dev",
    "namespace": "slo-rollout",
    "version": "v-test",
    "commit": "abc123"
  },
  "image": {
    "image": "registry.local/demo-app@sha256:111",
    "imageTag": null,
    "imageDigest": "sha256:111",
    "usesDigestReference": true,
    "usesMutableTag": false
  },
  "attestations": {
    "sbom": {
      "ref": "sbom.json"
    },
    "provenance": {
      "ref": "provenance.json"
    },
    "cosign": {
      "verified": true
    },
    "slsa": {
      "level": "1"
    }
  },
  "checks": [],
  "decision": {
    "decision": "ALLOW",
    "requiresHumanApproval": false,
    "allowed": true,
    "blockingReasons": [],
    "warningReasons": []
  },
  "risk": {
    "riskLevel": "low",
    "riskScore": 0
  },
  "guardrails": {
    "readOnly": true,
    "willExecute": false
  }
}
JSON

cat > "$FAKE_PASS" <<'SH_FAKE_PASS'
#!/usr/bin/env bash
echo "fake cosign pass: $*"
exit 0
SH_FAKE_PASS

cat > "$FAKE_FAIL" <<'SH_FAKE_FAIL'
#!/usr/bin/env bash
echo "fake cosign fail: $*"
exit 7
SH_FAKE_FAIL

chmod +x "$FAKE_PASS" "$FAKE_FAIL"

echo "===== syntax checks ====="
bash -n scripts/build-signed-release-gate.sh
bash -n scripts/test-verification-gate-decision.sh
"$PYTHON_BIN" -m py_compile scripts/verification-runtime.py
rm -rf scripts/__pycache__

echo "===== external verification success should allow gate ====="
SIGNED_RELEASE_GATE_OUTPUT_FILE="$PASS_GATE" \
SIGNED_RELEASE_GATE_VERIFICATION_MODE="external_command" \
S_SENTINEL_VERIFICATION_ALLOW_EXTERNAL_COMMAND=1 \
S_SENTINEL_COSIGN_BIN="$FAKE_PASS" \
  scripts/build-signed-release-gate.sh "$SUPPLY_CHAIN"

echo "===== external verification failure should block gate ====="
SIGNED_RELEASE_GATE_OUTPUT_FILE="$FAIL_GATE" \
SIGNED_RELEASE_GATE_VERIFICATION_MODE="external_command" \
S_SENTINEL_VERIFICATION_ALLOW_EXTERNAL_COMMAND=1 \
S_SENTINEL_COSIGN_BIN="$FAKE_FAIL" \
  scripts/build-signed-release-gate.sh "$SUPPLY_CHAIN"

echo "===== assert gate decisions ====="
"$PYTHON_BIN" - "$PASS_GATE" "$FAIL_GATE" "$FAKE_PASS" "$FAKE_FAIL" <<'PY'
import json
import sys
from pathlib import Path

pass_gate = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
fail_gate = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
fake_pass = sys.argv[3]
fake_fail = sys.argv[4]

pass_checks = {item["checkId"]: item for item in pass_gate["checks"]}
fail_checks = {item["checkId"]: item for item in fail_gate["checks"]}

assert pass_gate["decision"]["decision"] == "ALLOW", pass_gate
assert pass_gate["decision"]["allowed"] is True, pass_gate
assert pass_gate["decision"]["requiresHumanApproval"] is False, pass_gate
assert pass_gate["risk"]["riskLevel"] == "low", pass_gate
assert pass_checks["external_verification_succeeded"]["status"] == "PASS", pass_checks
assert pass_gate["verification"]["results"]["externalVerificationExecuted"] is True, pass_gate
assert pass_gate["verification"]["results"]["externalVerificationSucceeded"] is True, pass_gate
assert pass_gate["verification"]["verificationStatus"] == "external_verification_passed", pass_gate
assert pass_gate["verification"]["command"] == [fake_pass, "verify", "registry.local/demo-app@sha256:111"], pass_gate
assert pass_gate["verification"]["exitCode"] == 0, pass_gate

assert fail_gate["decision"]["decision"] == "BLOCK", fail_gate
assert fail_gate["decision"]["allowed"] is False, fail_gate
assert fail_gate["decision"]["requiresHumanApproval"] is True, fail_gate
assert fail_gate["risk"]["riskLevel"] in {"high", "critical"}, fail_gate
assert fail_checks["external_verification_failed"]["status"] == "FAIL", fail_checks
assert fail_gate["verification"]["results"]["externalVerificationExecuted"] is True, fail_gate
assert fail_gate["verification"]["results"]["externalVerificationSucceeded"] is False, fail_gate
assert fail_gate["verification"]["verificationStatus"] == "external_verification_failed", fail_gate
assert fail_gate["verification"]["command"] == [fake_fail, "verify", "registry.local/demo-app@sha256:111"], fail_gate
assert fail_gate["verification"]["exitCode"] == 7, fail_gate
assert "External verification command failed" in fail_gate["decision"]["blockingReasons"], fail_gate

print("PASS: external verification result is wired into signed release gate decision")
PY

echo "PASS: Policy Runtime Contract verification gate decision test passed"
