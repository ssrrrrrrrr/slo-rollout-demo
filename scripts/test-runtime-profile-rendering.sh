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

TEST_ROOT="${TEST_ROOT:-/tmp/ssentinel-compiler-profile-rendering-runtime-profile-rendering}"
PROFILE="$TEST_ROOT/demo-app-runtime-shape-test.profile.yaml"
OUT="$TEST_ROOT/out"

rm -rf "$TEST_ROOT"
mkdir -p "$TEST_ROOT" "$OUT"

cp configs/compiler-profiles/demo-app.profile.yaml "$PROFILE"

echo "===== mutate CompilerProfile runtime/service shape ====="
"$PYTHON_BIN" - "$PROFILE" <<'PY'
import sys
from pathlib import Path
import yaml

path = Path(sys.argv[1])
data = yaml.safe_load(path.read_text(encoding="utf-8"))

data["metadata"]["name"] = "demo-app-runtime-shape-test-profile"

service = data["spec"]["serviceConfig"]
service["containerName"] = "demo-app-runtime"
service["servicePortName"] = "web"
service["containerPort"] = 18080
service["health"]["readinessPath"] = "/readyz"
service["health"]["livenessPath"] = "/livez"

runtime = data["spec"]["runtimeProfile"]
runtime["replicas"] = 5
runtime["revisionHistoryLimit"] = 7
runtime["imagePullPolicy"] = "Always"

path.write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True), encoding="utf-8")
PY

echo "===== compile with mutated runtime profile ====="
./scripts/compile-release-config.sh \
  --env dev \
  --compiler-profile "$PROFILE" \
  --image-tag "v46-runtime-profile" \
  --app-version "v46" \
  --fault-rate "0" \
  --latency-ms "0" \
  --output-dir "$OUT"

echo "===== kustomize compiled mutated profile ====="
kubectl kustomize "$OUT/dev" >/tmp/ssentinel-compiler-profile-rendering-runtime-profile-rendering.yaml
grep -q "kind: Rollout" /tmp/ssentinel-compiler-profile-rendering-runtime-profile-rendering.yaml
grep -q "containerPort: 18080" /tmp/ssentinel-compiler-profile-rendering-runtime-profile-rendering.yaml

echo "===== assert runtime profile drives rendered workload shape ====="
"$PYTHON_BIN" - "$PROFILE" "$OUT/dev" <<'PY'
import json
import sys
from pathlib import Path
import yaml

profile_path = Path(sys.argv[1])
env_dir = Path(sys.argv[2])

profile = yaml.safe_load(profile_path.read_text(encoding="utf-8"))
rollout = yaml.safe_load((env_dir / "rollout.yaml").read_text(encoding="utf-8"))
plan = json.loads((env_dir / "rendered-release-plan.json").read_text(encoding="utf-8"))

service = profile["spec"]["serviceConfig"]
runtime = profile["spec"]["runtimeProfile"]
container = rollout["spec"]["template"]["spec"]["containers"][0]

assert rollout["spec"]["replicas"] == 5, rollout["spec"]
assert rollout["spec"]["revisionHistoryLimit"] == 7, rollout["spec"]

assert container["name"] == "demo-app-runtime", container
assert container["imagePullPolicy"] == "Always", container
assert container["ports"][0]["name"] == "web", container["ports"]
assert container["ports"][0]["containerPort"] == 18080, container["ports"]
assert container["readinessProbe"]["httpGet"]["path"] == "/readyz", container
assert container["readinessProbe"]["httpGet"]["port"] == 18080, container
assert container["livenessProbe"]["httpGet"]["path"] == "/livez", container
assert container["livenessProbe"]["httpGet"]["port"] == 18080, container

compiler_profile = plan["compilerProfile"]
assert compiler_profile["profileId"] == "demo-app-runtime-shape-test-profile", compiler_profile
assert compiler_profile["profileRef"] == str(profile_path), compiler_profile
assert compiler_profile["serviceConfig"] == service, compiler_profile
assert compiler_profile["runtimeProfile"] == runtime, compiler_profile
assert compiler_profile["guardrails"]["drivesRenderedWorkloadShape"] is True, compiler_profile
assert compiler_profile["guardrails"]["doesNotApplyKubernetes"] is True, compiler_profile
assert plan["guardrails"]["doesNotApplyKubernetes"] is True, plan["guardrails"]
assert plan["guardrails"]["doesNotCommitOrPush"] is True, plan["guardrails"]

print("PASS: RuntimeProfile drives rendered Rollout workload shape")
PY

echo "PASS: Compiler Profile and Rendering runtime profile rendering test passed"
