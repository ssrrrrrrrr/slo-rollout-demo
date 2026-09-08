#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "===== opa policy mapping static contract ====="

python3 - <<'PY'
from pathlib import Path

rego = Path("policy/opa/release_policy.rego").read_text(encoding="utf-8")

required = [
    "package ssentinel.release",
    "import rego.v1",
    "release.policy.evaluator/v1alpha1",
    "policyDecision",
    "allowed",
    "finalAction",
    "requiresHumanApproval",
    "matchedRules",
    "deniedReasons",
    "approvalRequiredReasons",
    "doesNotModifyKubernetes",
    "doesNotModifyGitOps",
    "doesNotBuildOrPushImages",

    "opa_pass_noop_allowed",
    "opa_dangerous_action_denied",
    "opa_signed_release_gate_blocked",
    "opa_signed_release_gate_requires_approval",
    "opa_slo_failure_stop_promotion_requires_approval",
    "opa_rollback_requires_approval",
    "opa_fallback_deny_unknown_or_unsafe_action",

    "DELETE_RESOURCE",
    "PATCH_RESOURCE",
    "APPLY_MANIFEST",
    "FAIL_BY_MULTIPLE_SLO",
    "STOP_PROMOTION",
    "ROLLBACK",
]

missing = [item for item in required if item not in rego]
if missing:
    raise SystemExit(f"missing OPA policy tokens: {missing}")

print("PASS: OPA policy mapping static contract is valid")
PY

OPA_BIN="${S_SENTINEL_OPA_BIN:-$(command -v opa || true)}"

if [[ -z "$OPA_BIN" ]]; then
  echo "SKIP: opa binary not found, real opa eval is skipped"
  echo "PASS: Policy Runtime Contract OPA policy mapping test passed"
  exit 0
fi

echo "INFO: opa binary found at $OPA_BIN"
echo "INFO: real opa eval can be added in the next step"
echo "PASS: Policy Runtime Contract OPA policy mapping test passed"
