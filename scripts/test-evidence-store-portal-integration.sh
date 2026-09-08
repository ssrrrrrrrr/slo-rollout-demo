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

TMP_DIR="${TMP_DIR:-/tmp/ssentinel-evidence-store-portal-integration-evidence-store-test}"
DB_FILE="$TMP_DIR/evidence-store.db"
REPORT_DIR="${REPORT_DIR:-/data/nfs/slo-rollout-watcher/reports}"

if [ ! -d "$REPORT_DIR" ]; then
  REPORT_DIR="docs/release-reports"
fi

rm -rf "$TMP_DIR"
mkdir -p "$TMP_DIR"

section() {
  echo
  echo "===== $* ====="
}

assert_file_contains() {
  local file="$1"
  local pattern="$2"
  local message="$3"

  if grep -q "$pattern" "$file"; then
    echo "PASS: $message"
  else
    echo "FAIL: $message" >&2
    echo "missing pattern: $pattern" >&2
    exit 1
  fi
}

section "EvidenceStore Portal Integration syntax checks"
"$PYTHON_BIN" -m py_compile scripts/evidence-store.py
bash -n scripts/test-evidence-store.sh
section "EvidenceStore Portal Integration real reports import"
./scripts/evidence-store.py init-db --db "$DB_FILE" > "$TMP_DIR/init-db.json"
./scripts/evidence-store.py import-dir --db "$DB_FILE" --report-dir "$REPORT_DIR" > "$TMP_DIR/import-dir.json"
cat "$TMP_DIR/import-dir.json"

"$PYTHON_BIN" - "$TMP_DIR/import-dir.json" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
assert data["importedObjects"] >= 1, data
assert data["releaseCount"] >= 1, data
assert "releaseEvidence" in data["byType"], data
print("PASS: real report import result is valid")
PY

section "EvidenceStore Portal Integration real EvidenceStore object graph"
"$PYTHON_BIN" - "$DB_FILE" "$TMP_DIR/evidence-store-portal-integration-release-id.txt" <<'PY'
import sqlite3
import sys
from pathlib import Path

db, out = sys.argv[1], sys.argv[2]
conn = sqlite3.connect(db)

expected = {
    "releaseEvidence",
    "evidenceRecord",
    "agentRun",
    "planRun",
    "executionRequest",
    "supplyChainDecision",
}

rows = conn.execute(
    """
    SELECT release_id
    FROM evidence_objects
    GROUP BY release_id
    HAVING COUNT(DISTINCT object_type) >= 6
    ORDER BY release_id DESC
    LIMIT 1
    """
).fetchall()

if not rows:
    raise SystemExit("no release with full EvidenceStore Portal Integration object graph found")

release_id = rows[0][0]
actual = {
    row[0]
    for row in conn.execute(
        "SELECT DISTINCT object_type FROM evidence_objects WHERE release_id = ?",
        (release_id,),
    ).fetchall()
}

missing = expected - actual
if missing:
    raise SystemExit(f"release {release_id} missing object types: {sorted(missing)}")

Path(out).write_text(release_id, encoding="utf-8")
print(f"PASS: release {release_id} has full EvidenceStore Portal Integration object graph")
PY

RELEASE_ID="$(cat "$TMP_DIR/evidence-store-portal-integration-release-id.txt")"

./scripts/evidence-store.py query-release \
  --db "$DB_FILE" \
  --release-id "$RELEASE_ID" > "$TMP_DIR/query-release.json"

./scripts/evidence-store.py list-releases \
  --db "$DB_FILE" \
  --limit 5 > "$TMP_DIR/list-releases.json"

SUPPLY_CHAIN_ID="$("$PYTHON_BIN" - "$DB_FILE" "$RELEASE_ID" <<'PY'
import sqlite3
import sys

db, release_id = sys.argv[1], sys.argv[2]
conn = sqlite3.connect(db)
row = conn.execute(
    """
    SELECT object_id
    FROM evidence_objects
    WHERE release_id = ?
      AND object_type = 'supplyChainDecision'
    ORDER BY object_id
    LIMIT 1
    """,
    (release_id,),
).fetchone()
if not row:
    raise SystemExit("missing supplyChainDecision object")
print(row[0])
PY
)"

./scripts/evidence-store.py get-object \
  --db "$DB_FILE" \
  --object-type supplyChainDecision \
  --object-id "$SUPPLY_CHAIN_ID" \
  --release-id "$RELEASE_ID" > "$TMP_DIR/get-object.json"

"$PYTHON_BIN" - "$TMP_DIR/query-release.json" "$TMP_DIR/list-releases.json" "$TMP_DIR/get-object.json" <<'PY'
import json
import sys
from pathlib import Path

query_release = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
list_releases = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
obj = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8"))

assert query_release["schemaVersion"] == "evidence.store.release/v1alpha1", query_release
assert query_release["objectCount"] >= 6, query_release["objectCount"]
assert list_releases["schemaVersion"] == "evidence.store.releaseList/v1alpha1", list_releases
assert isinstance(list_releases["items"], list), list_releases
assert obj["schemaVersion"] == "evidence.store.object/v1alpha1", obj
assert obj["object"]["object_type"] == "supplyChainDecision", obj

print("PASS: real EvidenceStore query APIs are valid")
PY

section "EvidenceStore Portal Integration Portal adapter Go test"
(
  cd watcher
  go test ./...
)

echo "PASS: watcher Go tests passed"

section "EvidenceStore Portal Integration API documentation checks"
assert_file_contains docs/release-portal-api.md "/api/evidence-store/releases" "docs include EvidenceStore release list endpoint"
assert_file_contains docs/release-portal-api.md "/api/evidence-store/releases/{releaseId}" "docs include EvidenceStore release detail endpoint"
assert_file_contains docs/release-portal-api.md "/api/evidence-store/objects/{objectType}/{objectId}" "docs include EvidenceStore object endpoint"

section "EvidenceStore Portal Integration acceptance result"
echo "PASS: EvidenceStore Portal Integration Evidence API / Evidence Store acceptance passed"
