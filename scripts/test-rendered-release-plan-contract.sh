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

TEST_OUT="${TEST_OUT:-/tmp/ssentinel-compiler-profile-rendering-rendered-release-plan-contract}"
rm -rf "$TEST_OUT"
mkdir -p "$TEST_OUT"

echo "===== compile dev for RenderedReleasePlan contract ====="
./scripts/compile-release-config.sh \
  --env dev \
  --image-tag "v46-rendered-plan" \
  --app-version "v46" \
  --fault-rate "0" \
  --latency-ms "0" \
  --output-dir "$TEST_OUT"

echo "===== assert RenderedReleasePlan contract ====="
"$PYTHON_BIN" - "$TEST_OUT/dev/rendered-release-plan.json" "schemas/rendered-release-plan.schema.json" <<'PY'
import json
import sys
from pathlib import Path

plan_path = Path(sys.argv[1])
schema_path = Path(sys.argv[2])

plan = json.loads(plan_path.read_text(encoding="utf-8"))
schema = json.loads(schema_path.read_text(encoding="utf-8"))

assert schema["properties"]["schemaVersion"]["const"] == "ssentinel.rendered-release-plan/v1alpha1", schema
assert schema["properties"]["kind"]["const"] == "RenderedReleasePlan", schema
assert schema["properties"]["outputs"]["required"] == [
    "outputDir",
    "analysisTemplate",
    "rollout",
    "prometheusRule",
    "kustomization",
    "renderedReleasePlan",
    "rendererRefs",
    "artifacts",
], schema["properties"]["outputs"]["required"]

# Use jsonschema when available, but keep direct assertions as the source of the contract.
try:
    import jsonschema
except Exception:
    jsonschema = None

if jsonschema is not None:
    jsonschema.validate(instance=plan, schema=schema)

assert plan["schemaVersion"] == "ssentinel.rendered-release-plan/v1alpha1", plan
assert plan["kind"] == "RenderedReleasePlan", plan
assert plan["generatedBy"] == "scripts/compile-release-config.sh", plan

for section in [
    "release",
    "inputs",
    "sourceConfigRefs",
    "compilerProfile",
    "outputs",
    "guardrails",
]:
    assert section in plan, section

assert plan["release"]["service"] == "demo-app", plan["release"]
assert plan["release"]["env"] == "dev", plan["release"]
assert plan["release"]["namespace"] == "slo-rollout", plan["release"]
assert plan["release"]["imageTag"] == "v46-rendered-plan", plan["release"]

assert plan["inputs"]["environmentConfigRef"] == "configs/environments/dev.yaml", plan["inputs"]
assert plan["inputs"]["sloConfigRef"] == "configs/services/demo-app.slo.yaml", plan["inputs"]
assert plan["inputs"]["strategyConfigRef"] == "configs/services/demo-app.strategy.yaml", plan["inputs"]
assert plan["inputs"]["compilerProfileRef"] == "configs/compiler-profiles/demo-app.profile.yaml", plan["inputs"]

source_refs = plan["sourceConfigRefs"]
assert source_refs["environmentConfig"]["kind"] == "EnvironmentConfig", source_refs
assert source_refs["sloConfig"]["kind"] == "SLOConfig", source_refs
assert source_refs["progressiveDeliveryStrategy"]["kind"] == "ProgressiveDeliveryStrategy", source_refs
assert source_refs["compilerProfile"]["kind"] == "CompilerProfile", source_refs

compiler_profile = plan["compilerProfile"]
assert compiler_profile["enabled"] is True, compiler_profile
assert compiler_profile["profileId"] == "demo-app-compiler-profile", compiler_profile
assert compiler_profile["apiVersion"] == "compiler.ssentinel.io/v1alpha1", compiler_profile
assert compiler_profile["kind"] == "CompilerProfile", compiler_profile
assert compiler_profile["serviceConfig"]["serviceName"] == "demo-app", compiler_profile
assert compiler_profile["runtimeProfile"]["runtimeType"] == "container", compiler_profile
assert compiler_profile["metricBinding"]["provider"] == "prometheus", compiler_profile
assert compiler_profile["rendererRefs"]["rolloutTemplate"] == "argo-rollouts-canary-v1", compiler_profile
assert compiler_profile["guardrails"]["doesNotApplyKubernetes"] is True, compiler_profile

outputs = plan["outputs"]
assert outputs["analysisTemplate"] == "analysis.yaml", outputs
assert outputs["rollout"] == "rollout.yaml", outputs
assert outputs["prometheusRule"] == "prometheusrule.yaml", outputs
assert outputs["kustomization"] == "kustomization.yaml", outputs
assert outputs["renderedReleasePlan"] == "rendered-release-plan.json", outputs

renderer_refs = outputs["rendererRefs"]
assert renderer_refs["analysisTemplate"] == "prometheus-analysis-template-v1", renderer_refs
assert renderer_refs["rollout"] == "argo-rollouts-canary-v1", renderer_refs
assert renderer_refs["prometheusRule"] == "prometheus-rule-v1", renderer_refs
assert renderer_refs["environmentOverlay"] == "kustomize-overlay-v1", renderer_refs

artifacts = {item["kind"]: item for item in outputs["artifacts"]}
assert set(artifacts) == {"AnalysisTemplate", "Rollout", "PrometheusRule", "Kustomization"}, artifacts
assert artifacts["AnalysisTemplate"]["path"] == "analysis.yaml", artifacts
assert artifacts["Rollout"]["path"] == "rollout.yaml", artifacts
assert artifacts["PrometheusRule"]["path"] == "prometheusrule.yaml", artifacts
assert artifacts["Kustomization"]["path"] == "kustomization.yaml", artifacts
assert artifacts["AnalysisTemplate"]["rendererRef"] == renderer_refs["analysisTemplate"], artifacts
assert artifacts["Rollout"]["rendererRef"] == renderer_refs["rollout"], artifacts
assert artifacts["PrometheusRule"]["rendererRef"] == renderer_refs["prometheusRule"], artifacts
assert artifacts["Kustomization"]["rendererRef"] == renderer_refs["environmentOverlay"], artifacts

guardrails = plan["guardrails"]
assert guardrails["doesNotApplyKubernetes"] is True, guardrails
assert guardrails["doesNotCommitOrPush"] is True, guardrails
assert guardrails["doesNotBuildImages"] is True, guardrails
assert guardrails["doesNotModifyCluster"] is True, guardrails

print("PASS: RenderedReleasePlan schema and contract are valid")
PY

echo "PASS: Compiler Profile and Rendering RenderedReleasePlan contract test passed"
