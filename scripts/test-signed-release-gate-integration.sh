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

echo "===== Signed Release Gate Integration syntax checks ====="
"$PYTHON_BIN" -m py_compile scripts/policy-runtime-adapter.py scripts/evidence-store.py
bash -n scripts/ai-release-advisor.sh
bash -n scripts/build-signed-release-gate.sh
bash -n scripts/build-supply-chain-decision.sh
bash -n scripts/evaluate-agent-decision.sh
bash -n scripts/test-signed-release-gate.sh
bash -n scripts/test-policy-runtime-adapter.sh
bash -n scripts/test-policy-guard.sh
bash -n scripts/test-policy-runtime-adapter-integration.sh
rm -rf scripts/__pycache__
echo "PASS: Signed Release Gate Integration syntax checks passed"

echo
echo "===== Signed Release Gate Integration SignedReleaseGate contract and EvidenceStore regression ====="
./scripts/test-signed-release-gate.sh
rm -rf scripts/__pycache__
echo "PASS: SignedReleaseGate contract and EvidenceStore regression passed"

echo
echo "===== Signed Release Gate Integration PolicyRuntime signed gate regression ====="
./scripts/test-policy-runtime-adapter.sh
rm -rf scripts/__pycache__
echo "PASS: PolicyRuntime signed gate regression passed"

echo
echo "===== Signed Release Gate Integration Policy Guard signed gate regression ====="
./scripts/test-policy-guard.sh
echo "PASS: Policy Guard signed gate regression passed"

echo
echo "===== Signed Release Gate Integration advisor pipeline wiring checks ====="
grep -q "SIGNED_RELEASE_GATE_BUILDER" scripts/ai-release-advisor.sh
grep -q "build-signed-release-gate.sh" scripts/ai-release-advisor.sh
grep -q "SIGNED_RELEASE_GATE_OUTPUT_DIR" scripts/ai-release-advisor.sh
grep -q "supply-chain-decision-\${DECISION_SUFFIX}" scripts/ai-release-advisor.sh
grep -q "Running signed release gate builder" scripts/ai-release-advisor.sh
echo "PASS: advisor pipeline wiring checks passed"

echo
echo "===== Signed Release Gate Integration evidence object indexing checks ====="
grep -q '"object_type": "signedReleaseGate"' scripts/evidence-store.py
grep -q 'signed.release.gate/v1alpha1' scripts/evidence-store.py
grep -q 'signed-release-gate-\*.json' scripts/evidence-store.py
grep -q 'signedReleaseGateId' scripts/evidence-store.py
echo "PASS: signedReleaseGate EvidenceStore indexing checks passed"

echo
echo "===== Signed Release Gate Integration policy enforcement checks ====="
grep -q -- "--signed-release-gate" scripts/policy-runtime-adapter.py
grep -q "signedReleaseGateRef" scripts/policy-runtime-adapter.py
grep -q "signed_release_gate_requires_human_approval" scripts/evaluate-agent-decision.sh
grep -q "signed_release_gate_blocked" scripts/evaluate-agent-decision.sh
grep -q 'signed_gate_decision == "BLOCK"' scripts/evaluate-agent-decision.sh
grep -q 'signed_gate_decision == "REQUIRE_HUMAN_APPROVAL"' scripts/evaluate-agent-decision.sh
echo "PASS: signed gate policy enforcement checks passed"

echo
echo "===== policy runtime adapter compatibility regression ====="
./scripts/test-policy-runtime-adapter-integration.sh
rm -rf scripts/__pycache__
echo "PASS: Policy Runtime Adapter Integration compatibility regression passed"

echo
echo "PASS: Signed Release Gate Integration Signed Release Gate acceptance passed"
