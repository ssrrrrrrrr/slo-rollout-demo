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

TEST_ROOT="${TEST_ROOT:-/tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering}"
PROFILE="$TEST_ROOT/demo-app-metric-binding-test.profile.yaml"
OUT="$TEST_ROOT/out"

rm -rf "$TEST_ROOT"
mkdir -p "$TEST_ROOT" "$OUT"

cp configs/compiler-profiles/demo-app.profile.yaml "$PROFILE"

echo "===== mutate CompilerProfile metric binding ====="
"$PYTHON_BIN" - "$PROFILE" <<'PY'
import sys
from pathlib import Path
import yaml

path = Path(sys.argv[1])
data = yaml.safe_load(path.read_text(encoding="utf-8"))

data["metadata"]["name"] = "demo-app-metric-binding-test-profile"

metric = data["spec"]["metricBinding"]
metric["bindingSource"] = "CompilerProfile.spec.metricBinding.prometheus"
metric["prometheus"] = {
    "requestCounter": "custom_http_requests_total",
    "latencyHistogram": "custom_http_request_duration_seconds_bucket",
    "labels": {
        "namespace": "kubernetes_namespace",
        "version": "release_version",
        "status": "http_status",
    },
    "errorStatusRegex": "5[0-9][0-9]",
}

path.write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True), encoding="utf-8")
PY

echo "===== compile with mutated metric binding ====="
./scripts/compile-release-config.sh \
  --env dev \
  --compiler-profile "$PROFILE" \
  --image-tag "v46-metric-binding" \
  --app-version "v46" \
  --fault-rate "0" \
  --latency-ms "0" \
  --output-dir "$OUT"

echo "===== kustomize compiled mutated metric binding ====="
kubectl kustomize "$OUT/dev" >/tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering.yaml
grep -q "custom_http_requests_total" /tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering.yaml
grep -q "custom_http_request_duration_seconds_bucket" /tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering.yaml
grep -q "kubernetes_namespace" /tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering.yaml
grep -q "release_version" /tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering.yaml
grep -q "http_status" /tmp/ssentinel-compiler-profile-rendering-metric-binding-rendering.yaml

echo "===== assert metric binding drives rendered PromQL ====="
"$PYTHON_BIN" - "$PROFILE" "$OUT/dev" <<'PY'
import json
import sys
from pathlib import Path
import yaml

profile_path = Path(sys.argv[1])
env_dir = Path(sys.argv[2])

profile = yaml.safe_load(profile_path.read_text(encoding="utf-8"))
analysis = yaml.safe_load((env_dir / "analysis.yaml").read_text(encoding="utf-8"))
prometheus_rule = yaml.safe_load((env_dir / "prometheusrule.yaml").read_text(encoding="utf-8"))
plan = json.loads((env_dir / "rendered-release-plan.json").read_text(encoding="utf-8"))

metric_binding = profile["spec"]["metricBinding"]
prom = metric_binding["prometheus"]

plan_prom = plan["slo"]["observability"]["prometheus"]
assert plan_prom["provider"] == "prometheus", plan_prom
assert plan_prom["bindingSource"] == "CompilerProfile.spec.metricBinding.prometheus", plan_prom
assert plan_prom["requestCounter"] == prom["requestCounter"], plan_prom
assert plan_prom["latencyHistogram"] == prom["latencyHistogram"], plan_prom
assert plan_prom["errorStatusRegex"] == prom["errorStatusRegex"], plan_prom
assert plan_prom["labels"] == prom["labels"], plan_prom

compiler_profile = plan["compilerProfile"]
assert compiler_profile["profileId"] == "demo-app-metric-binding-test-profile", compiler_profile
assert compiler_profile["profileRef"] == str(profile_path), compiler_profile
assert compiler_profile["metricBinding"] == metric_binding, compiler_profile

analysis_queries = "\n".join(
    str(metric["provider"]["prometheus"]["query"])
    for metric in analysis["spec"]["metrics"]
)

rule_exprs = "\n".join(
    str(rule["expr"])
    for group in prometheus_rule["spec"]["groups"]
    for rule in group["rules"]
)

for expected in [
    "custom_http_requests_total",
    "custom_http_request_duration_seconds_bucket",
    'kubernetes_namespace="slo-rollout"',
    'release_version="v46-metric-binding"',
    'http_status=~"5[0-9][0-9]"',
]:
    assert expected in analysis_queries, expected + "\n" + analysis_queries

for expected in [
    "custom_http_requests_total",
    "custom_http_request_duration_seconds_bucket",
    'kubernetes_namespace="slo-rollout"',
    'http_status=~"5[0-9][0-9]"',
]:
    assert expected in rule_exprs, expected + "\n" + rule_exprs

assert "demo_http_requests_total" not in analysis_queries, analysis_queries
assert "demo_http_request_duration_seconds_bucket" not in analysis_queries, analysis_queries

assert plan["guardrails"]["doesNotApplyKubernetes"] is True, plan["guardrails"]
assert plan["guardrails"]["doesNotCommitOrPush"] is True, plan["guardrails"]

print("PASS: MetricBinding drives rendered Prometheus queries")
PY

echo "PASS: Compiler Profile and Rendering metric binding rendering test passed"
