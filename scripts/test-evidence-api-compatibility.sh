#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PYTHON_BIN="${PYTHON_BIN:-python3}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

db="$tmpdir/evidence-store.db"
reportdir="$tmpdir/reports"
release_id="20260101-000000"

mkdir -p "$reportdir"

cat > "$reportdir/release-evidence-${release_id}.json" <<JSON
{
  "schemaVersion": "release.evidence.bundle/v1alpha1",
  "generatedBy": "test-evidence-api-compatibility.sh",
  "releaseResult": "PASS",
  "policyDecision": "ALLOW",
  "finalAction": "NOOP",
  "executionMode": "advisory_only",
  "requiresHumanApproval": false,
  "safeToRetry": true,
  "service": "demo-app",
  "namespace": "slo-rollout",
  "env": "dev",
  "summary": {
    "riskLevel": "low",
    "riskScore": 0
  },
  "artifacts": {}
}
JSON

cat > "$reportdir/signed-release-gate-${release_id}.json" <<JSON
{
  "schemaVersion": "signed.release.gate/v1alpha1",
  "signedReleaseGateId": "srg-${release_id}",
  "release": {
    "releaseId": "${release_id}",
    "service": "demo-app",
    "namespace": "slo-rollout",
    "env": "dev"
  },
  "verification": {
    "schemaVersion": "signed.release.gate.verification/v1alpha1",
    "mode": "input_derived",
    "tool": "cosign",
    "toolBinary": "cosign",
    "toolAvailable": false,
    "results": {
      "signatureVerified": false,
      "sbomPresent": true,
      "provenancePresent": true,
      "slsaLevelPresent": "unknown"
    },
    "guardrails": {
      "canRunExternalVerification": false,
      "doesNotRunExternalCommands": true
    }
  }
}
JSON

run_json() {
  local name="$1"
  shift

  echo "===== ${name} ====="
  "$@" | tee "$tmpdir/${name}.json"
}

assert_schema() {
  local name="$1"
  local expected="$2"

  "$PYTHON_BIN" - "$tmpdir/${name}.json" "$expected" <<'PY'
import json
import sys

path, expected = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as f:
    data = json.load(f)

got = data.get("schemaVersion")
if got != expected:
    raise SystemExit(f"{path}: expected schemaVersion={expected}, got={got}")
PY
}

echo "===== python compile ====="
"$PYTHON_BIN" -m py_compile scripts/evidence-store.py

echo "===== watcher evidence service boundary ====="
(
  cd watcher
  go test ./...
)

echo "===== watcher cgo-disabled build compatibility ====="
(
  cd watcher
  CGO_ENABLED=0 go test ./...
)

grep -q "type EvidenceService" watcher/evidence_service.go
grep -q "type EvidenceRuntime" watcher/evidence_service.go
grep -q "type EvidenceRuntimeDescriptor" watcher/evidence_service.go
grep -q "cli-sqlite-runtime" watcher/evidence_service.go
grep -q "evidence.api.service/v1alpha1" watcher/evidence_service.go
grep -q "type EvidenceRepositoryDescriptor" watcher/evidence_repository.go
grep -q "evidence.repository/v1alpha1" watcher/evidence_repository.go
grep -q "NewEvidenceRepositoryForRuntime" watcher/evidence_repository.go
grep -q "NewCLIEvidenceRepository" watcher/evidence_repository.go
grep -q "NewNativeSQLiteEvidenceRepository" watcher/evidence_repository_native_sqlite.go
grep -q "native-sqlite-repository" watcher/evidence_repository_native_sqlite.go
grep -q "S_SENTINEL_EVIDENCE_REPOSITORY_MODE" watcher/evidence_repository.go
grep -q "modernc.org/sqlite" watcher/evidence_repository_native_sqlite.go
grep -q "SupportsNativeSQLite:        true" watcher/evidence_repository_native_sqlite.go
grep -q "SupportsGetRelease:          true" watcher/evidence_repository_native_sqlite.go
grep -q "SupportsListArtifacts:       true" watcher/evidence_repository_native_sqlite.go
grep -q "SupportsSearch:              true" watcher/evidence_repository_native_sqlite.go
grep -q "SupportsVerificationSummary: true" watcher/evidence_repository_native_sqlite.go
grep -q "SupportsGraph:               true" watcher/evidence_repository_native_sqlite.go
grep -q "func (repo \*NativeSQLiteEvidenceRepository) GetRelease" watcher/evidence_repository_native_sqlite.go
grep -q "func (repo \*NativeSQLiteEvidenceRepository) ListArtifacts" watcher/evidence_repository_native_sqlite.go
grep -q "func (repo \*NativeSQLiteEvidenceRepository) SearchObjects" watcher/evidence_repository_native_sqlite.go
grep -q "func (repo \*NativeSQLiteEvidenceRepository) GetVerificationSummary" watcher/evidence_repository_native_sqlite.go
grep -q "func (repo \*NativeSQLiteEvidenceRepository) GetGraph" watcher/evidence_repository_native_sqlite.go
grep -q "TestPortalEvidenceAPINativeSQLiteRepositoryIntegration" watcher/evidence_repository_native_sqlite_test.go
grep -q "X-S-Sentinel-Evidence-Repository-Type" watcher/evidence_repository_native_sqlite_test.go
grep -q "repositoryType\", \"native-sqlite" watcher/evidence_repository_native_sqlite_test.go
grep -q "X-S-Sentinel-Evidence-Runtime-Mode" watcher/portal_api.go
grep -q "TestPortalEvidenceStoreStatusRefreshRuntimeContract" watcher/evidence_api_status_refresh_contract_test.go
grep -q "evidence.store.schemaHealth/v1alpha1" watcher/evidence_api_status_refresh_contract_test.go
grep -q "evidence.store.refresh.error/v1alpha1" watcher/evidence_api_status_refresh_contract_test.go
grep -q "schemaCompatible" watcher/evidence_api_status_refresh_contract_test.go
grep -q "schemaReady" watcher/evidence_api_status_refresh_contract_test.go
grep -q "mutatesLocalEvidenceIndex" watcher/evidence_api_status_refresh_contract_test.go
grep -q "TestPortalEvidenceAPIResponseControlPlaneContractNativeSQLite" watcher/evidence_api_contract_test.go
grep -q "TestPortalEvidenceAPIErrorControlPlaneContractForNativeSchemaMismatch" watcher/evidence_api_contract_test.go
grep -q "requireEvidenceAPIControlPlaneContract" watcher/evidence_api_contract_test.go
grep -q "X-S-Sentinel-Evidence-Repository-Contract" watcher/evidence_api_contract_test.go
grep -q "evidence.store.schemaContract/v1alpha1" watcher/evidence_api_contract_test.go
grep -q "reject-native-read-query" watcher/evidence_api_contract_test.go
grep -q "X-S-Sentinel-Evidence-Repository-Type" watcher/portal_api.go
grep -q "ControlPlaneMetadata" watcher/evidence_service.go
grep -q "s-sentinel.io/evidence-api/v1alpha1" watcher/evidence_service.go
grep -q "evidence.api.response/v1alpha1" watcher/evidence_service.go
grep -q "encodeEvidenceRepositoryResponseBody" watcher/portal_api.go
grep -q "ControlPlaneMetadataForOperation" watcher/evidence_service.go
grep -q "doesNotModifyCluster" watcher/evidence_service.go
grep -q "doesNotModifyGitOps" watcher/evidence_service.go
grep -q "doesNotTriggerRollout" watcher/evidence_service.go
grep -q "mutatesLocalEvidenceIndex" watcher/evidence_service.go
grep -q "nativeSQLiteRepository" watcher/evidence_service.go
grep -q "repositoryDescriptor.SupportsNativeSQLite" watcher/evidence_service.go
grep -q "repositoryDescriptor.SupportsRemoteAPI" watcher/evidence_service.go
grep -q "schemaContract" watcher/evidence_service.go
grep -q "schemaHealth" watcher/evidence_service.go
grep -q "expectedEvidenceStoreSchemaID" watcher/evidence_schema_guard.go
grep -q "evidence.store.schemaHealth/v1alpha1" watcher/evidence_schema_guard.go
grep -q "evidence.store.schemaContract/v1alpha1" watcher/evidence_schema_guard.go
grep -q "verifySchemaCompatible" watcher/evidence_schema_guard.go
grep -q "PRAGMA user_version" watcher/evidence_schema_guard.go
grep -q "evidence_schema_migrations" watcher/evidence_schema_guard.go
grep -q "writeEvidenceStoreErrorWithOperation" watcher/portal_api.go

grep -q "controlPlane" docs/release-portal-api.md
grep -q "s-sentinel.io/evidence-api/v1alpha1" docs/release-portal-api.md
grep -q "evidence.api.response/v1alpha1" docs/release-portal-api.md
grep -q "cli-sqlite-runtime" docs/release-portal-api.md
grep -q "evidence.repository/v1alpha1" docs/release-portal-api.md

grep -q "controlPlane" scripts/validate-release-portal-api.sh
grep -q "s-sentinel.io/evidence-api/v1alpha1" scripts/validate-release-portal-api.sh
grep -q "evidence.api.response/v1alpha1" scripts/validate-release-portal-api.sh
grep -q "cli-sqlite-runtime" scripts/validate-release-portal-api.sh
grep -q "evidence.repository/v1alpha1" scripts/validate-release-portal-api.sh

grep -q "evidence.api.controlPlane/v1alpha1" schemas/evidence-api-control-plane.schema.json
grep -q "evidence.runtime/v1alpha1" schemas/evidence-runtime.schema.json
grep -q "evidence.repository/v1alpha1" schemas/evidence-repository.schema.json

echo "===== evidence api schema contracts ====="
"$PYTHON_BIN" - <<'PY_SCHEMA_CONTRACT'
import json
from pathlib import Path

schema_files = {
    "controlPlane": Path("schemas/evidence-api-control-plane.schema.json"),
    "runtime": Path("schemas/evidence-runtime.schema.json"),
    "repository": Path("schemas/evidence-repository.schema.json"),
}

schemas = {}
for name, path in schema_files.items():
    schemas[name] = json.loads(path.read_text(encoding="utf-8"))
    if schemas[name].get("type") != "object":
        raise SystemExit(f"{path}: expected object schema")
    if not schemas[name].get("required"):
        raise SystemExit(f"{path}: expected required fields")

runtime = {
    "runtimeId": "cli-sqlite",
    "runtimeType": "cli-backed-sqlite",
    "mode": "cli-sqlite-runtime",
    "legacyMode": "sqlite-adapter",
    "backend": "sqlite",
    "adapter": "python-cli",
    "storage": "local-file",
    "queryModel": "read-through-cli",
    "contractVersion": "evidence.runtime/v1alpha1",
    "readOnly": True,
    "willExecute": False,
    "supportsRefresh": True,
    "supportsSearch": True,
    "supportsGraph": True,
    "supportsVerificationSummary": True,
    "supportsNativeSQLite": False,
    "supportsRemoteApi": False,
}

repository = {
    "repositoryId": "cli-evidence-repository",
    "repositoryType": "cli-backed",
    "mode": "cli-repository",
    "runtimeMode": "cli-sqlite-runtime",
    "backend": "sqlite",
    "adapter": "python-cli",
    "storage": "local-file",
    "queryModel": "repository-through-runtime",
    "contractVersion": "evidence.repository/v1alpha1",
    "readOnly": True,
    "willExecute": False,
    "supportsListReleases": True,
    "supportsGetRelease": True,
    "supportsGetObject": True,
    "supportsListArtifacts": True,
    "supportsSearch": True,
    "supportsVerificationSummary": True,
    "supportsGraph": True,
    "supportsNativeSQLite": False,
    "supportsRemoteApi": False,
}

control_plane = {
    "schemaVersion": "evidence.api.controlPlane/v1alpha1",
    "apiVersion": "s-sentinel.io/evidence-api/v1alpha1",
    "contractVersion": "evidence.api.response/v1alpha1",
    "generatedAt": "2026-01-01T00:00:00Z",
    "generatedBy": "s-sentinel-evidence-api",
    "operation": "refresh",
    "service": {
        "name": "s-sentinel-evidence-api",
        "schemaVersion": "evidence.service/v1alpha1",
        "contractVersion": "evidence.api.service/v1alpha1",
        "role": "release-evidence-control-plane",
        "readOnly": True,
        "willExecute": False,
        "doesNotModifyCluster": True,
        "doesNotModifyGitOps": True,
        "doesNotTriggerRollout": True,
    },
    "runtime": runtime,
    "repository": repository,
    "paths": {},
    "capabilities": {},
    "dbFile": "/tmp/evidence-store.db",
    "runtimeMode": "cli-sqlite-runtime",
    "repositoryType": "cli-backed",
    "repositoryMode": "cli-repository",
    "repositoryContract": "evidence.repository/v1alpha1",
    "readOnly": True,
    "willExecute": False,
    "doesNotModifyCluster": True,
    "doesNotModifyGitOps": True,
    "doesNotTriggerRollout": True,
    "mutatesLocalEvidenceIndex": True,
    "mutationSemantics": {
        "doesNotModifyCluster": True,
        "doesNotModifyGitOps": True,
        "doesNotTriggerRollout": True,
        "mutatesLocalEvidenceIndex": True,
    },
}

def check_required(name, schema, instance):
    for key in schema.get("required", []):
        if key not in instance:
            raise SystemExit(f"{name}: missing required field {key}")

def check_consts(name, schema, instance):
    for key, prop in schema.get("properties", {}).items():
        if key in instance and "const" in prop and instance[key] != prop["const"]:
            raise SystemExit(f"{name}: expected {key}={prop['const']}, got {instance[key]}")

check_required("runtime", schemas["runtime"], runtime)
check_consts("runtime", schemas["runtime"], runtime)

check_required("repository", schemas["repository"], repository)
check_consts("repository", schemas["repository"], repository)

check_required("controlPlane", schemas["controlPlane"], control_plane)
check_consts("controlPlane", schemas["controlPlane"], control_plane)

if control_plane["runtime"]["contractVersion"] != "evidence.runtime/v1alpha1":
    raise SystemExit("controlPlane.runtime contractVersion mismatch")
if control_plane["repository"]["contractVersion"] != "evidence.repository/v1alpha1":
    raise SystemExit("controlPlane.repository contractVersion mismatch")
if control_plane["mutationSemantics"]["mutatesLocalEvidenceIndex"] is not True:
    raise SystemExit("controlPlane mutation semantics mismatch")

print("evidence-api-compatibility evidence api schema contract assertions passed")
PY_SCHEMA_CONTRACT

if grep -q "runEvidenceStoreCommand" watcher/portal_api.go; then
  echo "portal_api.go still owns EvidenceStore CLI runtime"
  exit 1
fi

# Legacy CLI compatibility.
run_json init-db "$PYTHON_BIN" scripts/evidence-store.py init-db --db "$db"
assert_schema init-db evidence.store.init/v1alpha1

run_json import-dir "$PYTHON_BIN" scripts/evidence-store.py import-dir --db "$db" --report-dir "$reportdir"
assert_schema import-dir evidence.store.import/v1alpha1

run_json list-releases "$PYTHON_BIN" scripts/evidence-store.py list-releases --db "$db" --limit 10
assert_schema list-releases evidence.store.releaseList/v1alpha1

run_json query-release "$PYTHON_BIN" scripts/evidence-store.py query-release --db "$db" --release-id "$release_id"
assert_schema query-release evidence.store.release/v1alpha1

run_json get-object "$PYTHON_BIN" scripts/evidence-store.py get-object --db "$db" --object-type releaseEvidence --object-id "re-${release_id}" --release-id "$release_id"
assert_schema get-object evidence.store.object/v1alpha1

# Evidence API Compatibility canonical/new CLI compatibility.
run_json schema "$PYTHON_BIN" scripts/evidence-store.py schema --db "$db"
assert_schema schema evidence.store.schema/v1alpha1

run_json list-artifacts "$PYTHON_BIN" scripts/evidence-store.py list-artifacts --db "$db" --release-id "$release_id"
assert_schema list-artifacts evidence.store.artifactList/v1alpha1

run_json search-objects "$PYTHON_BIN" scripts/evidence-store.py search-objects --db "$db" --query demo-app --limit 10
assert_schema search-objects evidence.store.search/v1alpha1

run_json verification-summary "$PYTHON_BIN" scripts/evidence-store.py verification-summary --db "$db" --release-id "$release_id"
assert_schema verification-summary evidence.store.verificationSummary/v1alpha1

run_json graph "$PYTHON_BIN" scripts/evidence-store.py graph --db "$db" --release-id "$release_id"
assert_schema graph evidence.store.graph/v1alpha1

echo "===== semantic assertions ====="
"$PYTHON_BIN" - "$tmpdir" "$release_id" <<'PY'
import json
import sys
from pathlib import Path

tmpdir = Path(sys.argv[1])
release_id = sys.argv[2]

def load(name):
    with open(tmpdir / f"{name}.json", encoding="utf-8") as f:
        return json.load(f)

import_result = load("import-dir")
if import_result.get("releaseCount") != 1:
    raise SystemExit(f"expected releaseCount=1, got {import_result}")
if import_result.get("importedObjects") != 2:
    raise SystemExit(f"expected importedObjects=2, got {import_result}")

release_list = load("list-releases")
if release_list.get("count", 0) < 1:
    raise SystemExit(f"expected at least one release, got {release_list}")

query_release = load("query-release")
if query_release.get("objectCount") != 2:
    raise SystemExit(f"expected objectCount=2, got {query_release}")

search = load("search-objects")
if search.get("count", 0) < 1:
    raise SystemExit(f"expected search count >= 1, got {search}")

verification = load("verification-summary")
latest = verification.get("latest") or {}
if latest.get("verificationMode") != "input_derived":
    raise SystemExit(f"expected verificationMode=input_derived, got {verification}")

expected_external_fields = {
    "externalVerificationRequested",
    "externalVerificationAllowed",
    "externalVerificationExecuted",
    "externalVerificationSucceeded",
    "externalVerificationSkippedReason",
}
missing_external_fields = sorted(field for field in expected_external_fields if field not in latest)
if missing_external_fields:
    raise SystemExit(f"verification summary missing external fields={missing_external_fields}, got {verification}")

graph = load("graph")
if graph.get("nodeCount", 0) < 3:
    raise SystemExit(f"expected graph nodeCount >= 3, got {graph}")
if graph.get("edgeCount", 0) < 2:
    raise SystemExit(f"expected graph edgeCount >= 2, got {graph}")

if graph.get("releaseId") != release_id:
    raise SystemExit(f"expected graph releaseId={release_id}, got {graph.get('releaseId')}")

print("evidence-api-compatibility evidence api compatibility assertions passed")
PY

echo "===== watcher API compatibility tests ====="
(
  cd watcher
  go test -run 'TestPortalEvidenceStoreAdapter|TestEvidenceStorePythonRuntimeEnvOverride' -v
)

echo "===== evidence-api-compatibility evidence api compatibility PASS ====="
