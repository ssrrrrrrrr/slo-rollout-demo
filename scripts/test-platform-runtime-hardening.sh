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

export PYTHON_BIN
export S_SENTINEL_PYTHON_BIN="${S_SENTINEL_PYTHON_BIN:-$PYTHON_BIN}"

section() {
  echo
  echo "===== $* ====="
}

section "Platform Runtime Hardening platform hardening runtime context"
echo "ROOT_DIR=$ROOT_DIR"
echo "PYTHON_BIN=$PYTHON_BIN"
echo "S_SENTINEL_PYTHON_BIN=$S_SENTINEL_PYTHON_BIN"
"$PYTHON_BIN" --version

section "Platform Runtime Hardening shell syntax checks"
bash -n scripts/test-evidence-store.sh
bash -n scripts/test-evidence-store-portal-integration.sh
bash -n scripts/test-policy-runtime-adapter-integration.sh
bash -n scripts/test-signed-release-gate-integration.sh
bash -n scripts/validate-release-portal-api.sh
bash -n scripts/validate-generated-release-contract.sh
bash -n scripts/validate-slo-config.sh
bash -n scripts/validate-progressive-delivery-strategy.sh
echo "PASS: shell syntax checks passed"

section "Platform Runtime Hardening Go adapter runtime regression"
(
  cd watcher
  S_SENTINEL_PYTHON_BIN="$S_SENTINEL_PYTHON_BIN" go test ./... -count=1
)
echo "PASS: Go EvidenceStore adapter runtime regression passed"

section "Platform Runtime Hardening config validator default resolver regression"
(
  unset PYTHON_BIN
  bash scripts/validate-slo-config.sh
  bash scripts/validate-progressive-delivery-strategy.sh
)
echo "PASS: config validators default resolver passed"

section "Platform Runtime Hardening config validator explicit PYTHON_BIN regression"
PYTHON_BIN="$PYTHON_BIN" bash scripts/validate-slo-config.sh
PYTHON_BIN="$PYTHON_BIN" bash scripts/validate-progressive-delivery-strategy.sh
echo "PASS: config validators explicit PYTHON_BIN passed"

section "Platform Runtime Hardening EvidenceStore CLI runtime regression"
(
  unset PYTHON_BIN
  bash scripts/test-evidence-store.sh > /tmp/s-sentinel-platform-runtime-hardening-evidence-store-default.log
)
PYTHON_BIN="$PYTHON_BIN" bash scripts/test-evidence-store.sh > /tmp/s-sentinel-platform-runtime-hardening-evidence-store-explicit.log

grep -q "PASS: evidence store test passed" /tmp/s-sentinel-platform-runtime-hardening-evidence-store-default.log
grep -q "PASS: evidence store test passed" /tmp/s-sentinel-platform-runtime-hardening-evidence-store-explicit.log
echo "PASS: EvidenceStore default and explicit runtime regressions passed"

section "EvidenceStore portal integration acceptance"
PYTHON_BIN="$PYTHON_BIN" bash scripts/test-evidence-store-portal-integration.sh
echo "PASS: EvidenceStore portal integration passed under the runtime boundary"

section "Policy runtime adapter integration acceptance"
PYTHON_BIN="$PYTHON_BIN" bash scripts/test-policy-runtime-adapter-integration.sh
echo "PASS: Policy Runtime Adapter integration passed under the runtime boundary"

if [ "${RUN_SIGNED_RELEASE_GATE_INTEGRATION:-0}" = "1" ]; then
  section "optional Signed Release Gate integration acceptance"
  PYTHON_BIN="$PYTHON_BIN" bash scripts/test-signed-release-gate-integration.sh
  echo "PASS: optional Signed Release Gate integration passed"
else
  section "optional Signed Release Gate integration acceptance skipped"
  echo "Set RUN_SIGNED_RELEASE_GATE_INTEGRATION=1 to include the full Signed Release Gate regression."
fi

section "Platform Runtime Hardening broken resolver guard"
if grep -RIn '"\$PYTHON_BIN" -m pip\|"\$PYTHON_BIN"-yaml\|"\$PYTHON_BIN"-jsonschema' \
  scripts/test-evidence-store-portal-integration.sh \
  scripts/test-evidence-store.sh \
  scripts/test-policy-runtime-adapter-integration.sh \
  scripts/test-signed-release-gate-integration.sh \
  scripts/validate-release-portal-api.sh \
  scripts/validate-generated-release-contract.sh \
  scripts/validate-slo-config.sh \
  scripts/validate-progressive-delivery-strategy.sh; then
  echo "FAIL: broken PYTHON_BIN string interpolation pattern found" >&2
  exit 1
fi

if grep -RIn 'if command -v "\$PYTHON_BIN" >/dev/null 2>&1; then' \
  scripts/test-evidence-store-portal-integration.sh \
  scripts/test-evidence-store.sh \
  scripts/test-policy-runtime-adapter-integration.sh \
  scripts/test-signed-release-gate-integration.sh \
  scripts/validate-release-portal-api.sh \
  scripts/validate-slo-config.sh \
  scripts/validate-progressive-delivery-strategy.sh; then
  echo "FAIL: broken PYTHON_BIN resolver pattern found" >&2
  exit 1
fi

echo "PASS: no broken PYTHON_BIN resolver patterns found"

section "Platform Runtime Hardening platform hardening acceptance result"
echo "PASS: Platform Runtime Hardening platform hardening runtime acceptance passed"
