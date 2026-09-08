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

TEST_OUT="${TEST_OUT:-/tmp/ssentinel-compiler-profile-rendering-compiler-profile-model}"
rm -rf "$TEST_OUT"
mkdir -p "$TEST_OUT"

echo "===== compile dev with compiler profile ====="
./scripts/compile-release-config.sh \
  --env dev \
  --image-tag "v46-profile" \
  --app-version "v46" \
  --fault-rate "0" \
  --latency-ms "0" \
  --output-dir "$TEST_OUT"

echo "===== kustomize compiled dev ====="
kubectl kustomize "$TEST_OUT/dev" >/tmp/ssentinel-compiler-profile-rendering-compiler-profile-model.yaml
grep -q "kind: Rollout" /tmp/ssentinel-compiler-profile-rendering-compiler-profile-model.yaml
grep -q "kind: AnalysisTemplate" /tmp/ssentinel-compiler-profile-rendering-compiler-profile-model.yaml
grep -q "kind: PrometheusRule" /tmp/ssentinel-compiler-profile-rendering-compiler-profile-model.yaml

echo "===== assert compiler profile model ====="
"$PYTHON_BIN" - "$TEST_OUT" <<'PY'
import json
import sys
from pathlib import Path

import yaml

out = Path(sys.argv[1])
profile_path = Path("configs/compiler-profiles/demo-app.profile.yaml")
schema_path = Path("schemas/compiler-profile.schema.json")

profile = yaml.safe_load(profile_path.read_text(encoding="utf-8"))
schema = json.loads(schema_path.read_text(encoding="utf-8"))
env = yaml.safe_load(Path("configs/environments/dev.yaml").read_text(encoding="utf-8"))

assert schema["properties"]["kind"]["const"] == "CompilerProfile", schema
assert profile["apiVersion"] == "compiler.ssentinel.io/v1alpha1", profile
assert profile["kind"] == "CompilerProfile", profile
assert profile["metadata"]["name"] == "demo-app-compiler-profile", profile
assert profile["metadata"]["service"] == "demo-app", profile

spec = profile["spec"]
for key in ["serviceConfig", "runtimeProfile", "metricBinding", "rendererRefs", "guardrails"]:
    assert key in spec, profile

assert spec["serviceConfig"]["containerPort"] == 8080, spec["serviceConfig"]
assert spec["runtimeProfile"]["replicas"] == 3, spec["runtimeProfile"]
assert spec["metricBinding"]["provider"] == "prometheus", spec["metricBinding"]
assert spec["metricBinding"]["bindingSource"] == "CompilerProfile.spec.metricBinding.prometheus", spec["metricBinding"]
assert spec["metricBinding"]["prometheus"]["requestCounter"] == "demo_http_requests_total", spec["metricBinding"]
assert spec["metricBinding"]["prometheus"]["latencyHistogram"] == "demo_http_request_duration_seconds_bucket", spec["metricBinding"]
assert spec["metricBinding"]["prometheus"]["labels"]["namespace"] == "namespace", spec["metricBinding"]
assert spec["metricBinding"]["prometheus"]["labels"]["version"] == "version", spec["metricBinding"]
assert spec["metricBinding"]["prometheus"]["labels"]["status"] == "status", spec["metricBinding"]
assert spec["metricBinding"]["prometheus"]["errorStatusRegex"] == "5..", spec["metricBinding"]
assert spec["rendererRefs"]["rolloutTemplate"] == "argo-rollouts-canary-v1", spec["rendererRefs"]
assert spec["rendererRefs"]["analysisTemplateRenderer"] == "prometheus-analysis-template-v1", spec["rendererRefs"]
assert spec["rendererRefs"]["environmentOverlayRenderer"] == "kustomize-overlay-v1", spec["rendererRefs"]
assert spec["guardrails"]["profileModelOnly"] is False, spec["guardrails"]
assert spec["guardrails"]["drivesRenderedWorkloadShape"] is True, spec["guardrails"]
assert spec["guardrails"]["doesNotApplyKubernetes"] is True, spec["guardrails"]

assert env["spec"]["compiler"]["defaultProfile"] == "demo-app-compiler-profile", env["spec"]["compiler"]
assert "configs/compiler-profiles/demo-app.profile.yaml" in env["spec"]["compiler"]["profileRefs"], env["spec"]["compiler"]

env_dir = out / "dev"
plan = json.loads((env_dir / "rendered-release-plan.json").read_text(encoding="utf-8"))
analysis = yaml.safe_load((env_dir / "analysis.yaml").read_text(encoding="utf-8"))
rollout = yaml.safe_load((env_dir / "rollout.yaml").read_text(encoding="utf-8"))
prometheus_rule = yaml.safe_load((env_dir / "prometheusrule.yaml").read_text(encoding="utf-8"))

compiler_profile = plan["compilerProfile"]
assert compiler_profile["enabled"] is True, compiler_profile
assert compiler_profile["profileId"] == "demo-app-compiler-profile", compiler_profile
assert compiler_profile["profileRef"] == "configs/compiler-profiles/demo-app.profile.yaml", compiler_profile
assert compiler_profile["serviceConfig"]["serviceName"] == "demo-app", compiler_profile
assert compiler_profile["runtimeProfile"]["runtimeType"] == "container", compiler_profile
assert compiler_profile["metricBinding"]["provider"] == "prometheus", compiler_profile
assert compiler_profile["metricBinding"]["bindingSource"] == "CompilerProfile.spec.metricBinding.prometheus", compiler_profile
assert compiler_profile["metricBinding"]["prometheus"]["requestCounter"] == "demo_http_requests_total", compiler_profile
assert compiler_profile["metricBinding"]["prometheus"]["latencyHistogram"] == "demo_http_request_duration_seconds_bucket", compiler_profile
assert compiler_profile["metricBinding"]["prometheus"]["errorStatusRegex"] == "5..", compiler_profile
assert compiler_profile["rendererRefs"]["prometheusRuleRenderer"] == "prometheus-rule-v1", compiler_profile
assert compiler_profile["guardrails"]["profileModelOnly"] is False, compiler_profile
assert compiler_profile["guardrails"]["drivesRenderedWorkloadShape"] is True, compiler_profile
assert compiler_profile["guardrails"]["doesNotApplyKubernetes"] is True, compiler_profile

assert plan["inputs"]["compilerProfileRef"] == "configs/compiler-profiles/demo-app.profile.yaml", plan["inputs"]
assert plan["sourceConfigRefs"]["compilerProfile"]["name"] == "demo-app-compiler-profile", plan["sourceConfigRefs"]

# 46.1 is model-only: rendered resource kinds and core names remain the same.
assert analysis["kind"] == "AnalysisTemplate", analysis
assert rollout["kind"] == "Rollout", rollout
assert prometheus_rule["kind"] == "PrometheusRule", prometheus_rule
assert analysis["metadata"]["name"] == "demo-app-error-rate", analysis["metadata"]
assert rollout["metadata"]["name"] == "demo-app", rollout["metadata"]
assert prometheus_rule["metadata"]["name"] == "demo-app-rollout-alerts", prometheus_rule["metadata"]

kustomization = yaml.safe_load((env_dir / "kustomization.yaml").read_text(encoding="utf-8"))

renderer_refs = spec["rendererRefs"]
assert analysis["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == renderer_refs["analysisTemplateRenderer"], analysis["metadata"]
assert rollout["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == renderer_refs["rolloutTemplate"], rollout["metadata"]
assert prometheus_rule["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == renderer_refs["prometheusRuleRenderer"], prometheus_rule["metadata"]
assert kustomization["metadata"]["annotations"]["ssentinel.io/renderer-ref"] == renderer_refs["environmentOverlayRenderer"], kustomization["metadata"]

assert analysis["metadata"]["annotations"]["ssentinel.io/output-kind"] == "AnalysisTemplate", analysis["metadata"]
assert rollout["metadata"]["annotations"]["ssentinel.io/output-kind"] == "Rollout", rollout["metadata"]
assert prometheus_rule["metadata"]["annotations"]["ssentinel.io/output-kind"] == "PrometheusRule", prometheus_rule["metadata"]
assert kustomization["metadata"]["annotations"]["ssentinel.io/output-kind"] == "Kustomization", kustomization["metadata"]

assert plan["outputs"]["rendererRefs"]["analysisTemplate"] == renderer_refs["analysisTemplateRenderer"], plan["outputs"]
assert plan["outputs"]["rendererRefs"]["rollout"] == renderer_refs["rolloutTemplate"], plan["outputs"]
assert plan["outputs"]["rendererRefs"]["prometheusRule"] == renderer_refs["prometheusRuleRenderer"], plan["outputs"]
assert plan["outputs"]["rendererRefs"]["environmentOverlay"] == renderer_refs["environmentOverlayRenderer"], plan["outputs"]

artifact_renderers = {item["kind"]: item["rendererRef"] for item in plan["outputs"]["artifacts"]}
assert artifact_renderers["AnalysisTemplate"] == renderer_refs["analysisTemplateRenderer"], artifact_renderers
assert artifact_renderers["Rollout"] == renderer_refs["rolloutTemplate"], artifact_renderers
assert artifact_renderers["PrometheusRule"] == renderer_refs["prometheusRuleRenderer"], artifact_renderers
assert artifact_renderers["Kustomization"] == renderer_refs["environmentOverlayRenderer"], artifact_renderers

prom = plan["slo"]["observability"]["prometheus"]
assert prom["provider"] == "prometheus", prom
assert prom["bindingSource"] == "CompilerProfile.spec.metricBinding.prometheus", prom
assert prom["requestCounter"] == "demo_http_requests_total", prom
assert prom["latencyHistogram"] == "demo_http_request_duration_seconds_bucket", prom
assert prom["errorStatusRegex"] == "5..", prom
assert prom["labels"]["namespace"] == "namespace", prom
assert prom["labels"]["version"] == "version", prom
assert prom["labels"]["status"] == "status", prom

container = rollout["spec"]["template"]["spec"]["containers"][0]
assert rollout["spec"]["replicas"] == spec["runtimeProfile"]["replicas"], rollout["spec"]
assert rollout["spec"]["revisionHistoryLimit"] == spec["runtimeProfile"]["revisionHistoryLimit"], rollout["spec"]
assert container["name"] == spec["serviceConfig"]["containerName"], container
assert container["imagePullPolicy"] == spec["runtimeProfile"]["imagePullPolicy"], container
assert container["ports"][0]["containerPort"] == spec["serviceConfig"]["containerPort"], container["ports"]
assert container["ports"][0]["name"] == spec["serviceConfig"]["servicePortName"], container["ports"]
assert container["readinessProbe"]["httpGet"]["path"] == spec["serviceConfig"]["health"]["readinessPath"], container
assert container["readinessProbe"]["httpGet"]["port"] == spec["serviceConfig"]["containerPort"], container
assert container["livenessProbe"]["httpGet"]["path"] == spec["serviceConfig"]["health"]["livenessPath"], container
assert container["livenessProbe"]["httpGet"]["port"] == spec["serviceConfig"]["containerPort"], container

print("PASS: Compiler Profile and Rendering CompilerProfile model is valid")
PY

echo "PASS: Compiler Profile and Rendering compiler profile model test passed"
