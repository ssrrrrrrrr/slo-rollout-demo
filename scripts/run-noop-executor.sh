#!/usr/bin/env bash
set -euo pipefail

REPORT_DIR="${RELEASE_REPORT_DIR:-docs/release-reports}"
INPUT_FILE="${1:-latest}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/run-noop-executor.sh [latest|RELEASE_EVIDENCE_JSON]

Environment:
  RELEASE_REPORT_DIR  Optional report directory.

Behavior:
  - Rebuilds execution preview for the target release evidence bundle.
  - Generates execution-result-*.json.
  - Rebuilds evidence-record-*.json so execution result becomes queryable.
  - Never modifies Kubernetes, GitOps, rollout state, images, commits, or pushes.
USAGE
}

if [ "${INPUT_FILE:-}" = "-h" ] || [ "${INPUT_FILE:-}" = "--help" ]; then
  usage
  exit 0
fi

if [ "$#" -gt 1 ]; then
  echo "ERROR: too many arguments" >&2
  usage >&2
  exit 1
fi

if [ "$INPUT_FILE" = "latest" ] || [ -z "$INPUT_FILE" ]; then
  INPUT_FILE="$(ls -t "$REPORT_DIR"/release-evidence-*.json 2>/dev/null | grep -v 'release-evidence-latest.json' | head -1 || true)"
fi

if [ -z "$INPUT_FILE" ] || [ ! -f "$INPUT_FILE" ]; then
  echo "ERROR: input file does not exist: ${INPUT_FILE:-not provided}" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PREVIEW_OUTPUT="$("$SCRIPT_DIR/build-execution-preview.sh" "$INPUT_FILE" | tail -n 1)"
RESULT_OUTPUT="$("$SCRIPT_DIR/build-execution-result.sh" "$INPUT_FILE" | tail -n 1)"
RECORD_OUTPUT="$("$SCRIPT_DIR/build-evidence-record.sh" "$INPUT_FILE" | tail -n 1)"

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

"$PYTHON_BIN" - "$INPUT_FILE" <<'PY'
from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

input_path = Path(sys.argv[1])
evidence = json.loads(input_path.read_text(encoding="utf-8-sig"))
artifacts = evidence.get("artifacts") if isinstance(evidence.get("artifacts"), dict) else {}
decision_refs = evidence.get("decisionRefs") if isinstance(evidence.get("decisionRefs"), dict) else {}
execution_result = decision_refs.get("executionResult") if isinstance(decision_refs.get("executionResult"), dict) else {}

print(json.dumps({
    "schemaVersion": "execution.noop.run/v1alpha1",
    "generatedAt": evidence.get("generatedAt"),
    "releaseId": evidence.get("releaseId"),
    "executionPreviewId": evidence.get("executionPreviewId"),
    "executionResultId": evidence.get("executionResultId"),
    "executionStatus": execution_result.get("executionStatus"),
    "readyForExecution": execution_result.get("readyForExecution"),
    "artifacts": {
        "releaseEvidence": str(input_path),
        "executionPreview": artifacts.get("executionPreview"),
        "executionResult": artifacts.get("executionResult"),
    },
    "guardrails": {
        "readOnly": False,
        "willExecute": False,
        "doesNotModifyKubernetes": True,
        "doesNotModifyGitOps": True,
        "doesNotTriggerRollout": True,
        "mutatesLocalEvidenceFiles": True,
    },
}, ensure_ascii=False, indent=2))
PY
