#!/usr/bin/env bash
set -euo pipefail

REPORT_DIR="${RELEASE_REPORT_DIR:-docs/release-reports}"
RELEASE_EVIDENCE_FILE="${1:-latest}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/build-evidence-record.sh [latest|RELEASE_EVIDENCE_JSON]

Environment:
  RELEASE_REPORT_DIR              Optional report directory.
  EVIDENCE_RECORD_OUTPUT_DIR      Optional output directory. Defaults to release evidence directory.

Behavior:
  - Reads release-evidence-*.json.
  - Generates evidence-record-<releaseId>.json and evidence-record-latest.json.
  - Builds a control-plane evidence index without executing Kubernetes, GitOps, rollback, promote, patch, or delete actions.
USAGE
}

if [ "$RELEASE_EVIDENCE_FILE" = "-h" ] || [ "$RELEASE_EVIDENCE_FILE" = "--help" ]; then
  usage
  exit 0
fi

if [ "$RELEASE_EVIDENCE_FILE" = "latest" ] || [ -z "$RELEASE_EVIDENCE_FILE" ]; then
  RELEASE_EVIDENCE_FILE="$(ls -t "$REPORT_DIR"/release-evidence-*.json 2>/dev/null | grep -v 'release-evidence-latest.json' | head -1 || true)"
fi

if [ -z "$RELEASE_EVIDENCE_FILE" ] || [ ! -f "$RELEASE_EVIDENCE_FILE" ]; then
  echo "ERROR: release evidence file does not exist: ${RELEASE_EVIDENCE_FILE:-not provided}" >&2
  exit 1
fi

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

OUTPUT_DIR="${EVIDENCE_RECORD_OUTPUT_DIR:-$(dirname "$RELEASE_EVIDENCE_FILE")}"
mkdir -p "$OUTPUT_DIR"

BASENAME="$(basename "$RELEASE_EVIDENCE_FILE")"
SUFFIX="${BASENAME#release-evidence-}"

if [ "$SUFFIX" = "$BASENAME" ]; then
  SUFFIX="$(date +%Y%m%d-%H%M%S).json"
fi

OUTPUT_JSON="$OUTPUT_DIR/evidence-record-$SUFFIX"
LATEST_JSON="$OUTPUT_DIR/evidence-record-latest.json"

"$PYTHON_BIN" - "$RELEASE_EVIDENCE_FILE" "$OUTPUT_JSON" "$LATEST_JSON" <<'PY'
from __future__ import annotations

import json
import shutil
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

evidence_path = Path(sys.argv[1])
output_json = Path(sys.argv[2])
latest_json = Path(sys.argv[3])

def now() -> str:
    return datetime.now(timezone.utc).isoformat()

def load_json(path: Path | None) -> dict[str, Any]:
    if not path:
        return {}
    try:
        data = json.loads(path.read_text(encoding="utf-8-sig"))
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}

def scalar(value: Any, fallback: str = "unknown") -> str:
    if value is None:
        return fallback
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, list):
        return ",".join(scalar(item) for item in value) if value else "none"
    return str(value)

def nullable_string(value: Any) -> str | None:
    if value is None:
        return None
    text = str(value).strip()
    return text if text else None

def as_dict(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}

def as_list(value: Any) -> list[Any]:
    if value is None:
        return []
    return value if isinstance(value, list) else [value]

def first_not_none(*values: Any) -> Any:
    for value in values:
        if value is not None:
            return value
    return None

def bool_or_none(value: Any) -> bool | None:
    if value is None:
        return None
    return bool(value)

def release_id_from_evidence(path: Path) -> str:
    base = path.name
    if base.startswith("release-evidence-") and base.endswith(".json"):
        return base[len("release-evidence-"):-len(".json")]
    return path.stem

def resolve_ref(ref: Any, source_path: Path) -> Path | None:
    if not ref:
        return None

    ref_path = Path(str(ref))
    candidates = [ref_path]

    if not ref_path.is_absolute():
        candidates.extend([
            Path.cwd() / ref_path,
            source_path.parent / ref_path.name,
            source_path.parent / ref_path,
        ])

    seen: set[str] = set()
    for candidate in candidates:
        key = str(candidate)
        if key in seen:
            continue
        seen.add(key)

        try:
            if candidate.exists() and candidate.is_file():
                return candidate
        except OSError:
            continue

    return None

def file_modified_at(path: Path | None) -> str | None:
    if not path:
        return None
    try:
        return datetime.fromtimestamp(path.stat().st_mtime, timezone.utc).isoformat()
    except OSError:
        return None

def file_size(path: Path | None) -> int | None:
    if not path:
        return None
    try:
        return path.stat().st_size
    except OSError:
        return None

def content_type(path: Path | None, kind: str) -> str | None:
    if not path:
        return None
    suffix = path.suffix.lower()
    if suffix == ".json":
        return "application/json"
    if suffix == ".md":
        return "text/markdown"
    if suffix in (".yaml", ".yml"):
        return "application/yaml"
    if kind:
        return "application/octet-stream"
    return None

def artifact_entry(kind: str, ref: Any, source_path: Path, required: bool = False) -> dict[str, Any]:
    resolved = resolve_ref(ref, source_path)
    return {
        "kind": kind,
        "path": str(resolved) if resolved else nullable_string(ref),
        "exists": resolved is not None,
        "required": required,
        "contentType": content_type(resolved, kind),
        "sizeBytes": file_size(resolved),
        "modifiedAt": file_modified_at(resolved),
    }

def objective_ids(snapshot: Any) -> list[str]:
    if not isinstance(snapshot, dict):
        return []
    spec = snapshot.get("spec") or {}
    objectives = spec.get("objectives") or []
    if not isinstance(objectives, list):
        return []
    ids: list[str] = []
    for item in objectives:
        if isinstance(item, dict) and item.get("id"):
            ids.append(str(item["id"]))
    return ids

def strategy_step_summaries(snapshot: Any) -> list[dict[str, Any]]:
    if not isinstance(snapshot, dict):
        return []
    spec = snapshot.get("spec") or {}
    traffic = spec.get("traffic") or {}
    steps = traffic.get("steps") or []
    if not isinstance(steps, list):
        return []

    result: list[dict[str, Any]] = []
    for item in steps:
        if not isinstance(item, dict):
            continue
        result.append({
            "name": nullable_string(item.get("name")),
            "setWeight": item.get("setWeight"),
            "pause": nullable_string(item.get("pause")),
        })
    return result

def strategy_spec_value(snapshot: Any, key: str) -> Any:
    if not isinstance(snapshot, dict):
        return None
    spec = snapshot.get("spec") or {}
    return spec.get(key)

evidence = load_json(evidence_path)
release_id = release_id_from_evidence(evidence_path)

artifacts = evidence.get("artifacts") if isinstance(evidence.get("artifacts"), dict) else {}
summary = evidence.get("summary") if isinstance(evidence.get("summary"), dict) else {}
decision_refs = evidence.get("decisionRefs") if isinstance(evidence.get("decisionRefs"), dict) else {}
policy_decision_ref = as_dict(decision_refs.get("policyDecision"))

release_context_path = resolve_ref(artifacts.get("releaseContext"), evidence_path)
release_context = load_json(release_context_path)

environment = as_dict(evidence.get("environment"))
environment_config_snapshot = evidence.get("environmentConfigSnapshot")

environment_config_ref = first_not_none(
    evidence.get("environmentConfigRef"),
    environment.get("configRef"),
    release_context.get("environmentConfigRef"),
)
service = evidence.get("service") or release_context.get("service") or release_context.get("rollout")
namespace = evidence.get("namespace") or environment.get("namespace") or release_context.get("namespace")
env = evidence.get("env") or environment.get("env") or release_context.get("env")
environment_profile = first_not_none(
    evidence.get("environmentProfile"),
    environment.get("profile"),
    release_context.get("environmentProfile"),
    env,
)
cluster_name = first_not_none(
    evidence.get("clusterName"),
    environment.get("clusterName"),
    release_context.get("clusterName"),
)
environment_class = first_not_none(
    evidence.get("environmentClass"),
    environment.get("environmentClass"),
    release_context.get("environmentClass"),
)
policy_profile = first_not_none(
    evidence.get("policyProfile"),
    environment.get("policyProfile"),
    release_context.get("policyProfile"),
)
gitops_overlay_path = first_not_none(
    evidence.get("gitopsOverlayPath"),
    environment.get("gitopsOverlayPath"),
    release_context.get("gitopsOverlayPath"),
)

version = (
    release_context.get("currentDesiredVersion")
    or release_context.get("version")
    or release_context.get("appVersion")
)

change_context = release_context.get("changeContext") if isinstance(release_context.get("changeContext"), dict) else {}
image_obj = change_context.get("image") if isinstance(change_context.get("image"), dict) else {}
image = image_obj.get("current") or image_obj.get("target") or image_obj.get("new") or image_obj.get("image")
commit = change_context.get("commit") or change_context.get("gitCommit")
image_digest = change_context.get("imageDigest") or image_obj.get("digest")

slo_snapshot = evidence.get("sloConfigSnapshot")
slo_id = evidence.get("sloId") or release_context.get("sloId")
slo_config_ref = evidence.get("sloConfigRef") or release_context.get("sloConfigRef")

strategy_snapshot = evidence.get("strategyConfigSnapshot")
strategy_id = evidence.get("strategyId") or release_context.get("strategyId")
strategy_config_ref = evidence.get("strategyConfigRef") or release_context.get("strategyConfigRef")
strategy_failure_policy = strategy_spec_value(strategy_snapshot, "failurePolicy")
strategy_promotion_policy = strategy_spec_value(strategy_snapshot, "promotionPolicy")

agent_run_path = resolve_ref(artifacts.get("agentRun"), evidence_path)
agent_run = load_json(agent_run_path)
agent_recommendation = as_dict(agent_run.get("recommendation"))
agent_guardrails = as_dict(agent_run.get("guardrails"))

plan_run_path = resolve_ref(artifacts.get("planRun"), evidence_path)
plan_run = load_json(plan_run_path)
plan_obj = as_dict(plan_run.get("plan"))
plan_retrieval = as_dict(plan_run.get("retrieval"))
plan_retrieval_summary = as_dict(plan_retrieval.get("summary"))
plan_guardrails = as_dict(plan_run.get("guardrails"))

execution_request_path = resolve_ref(artifacts.get("executionRequest"), evidence_path)
execution_request = load_json(execution_request_path)
execution_request_body = as_dict(execution_request.get("request"))
execution_policy_binding = as_dict(execution_request.get("policyBinding"))
execution_approval = as_dict(execution_request.get("approval"))
execution_evidence = as_dict(execution_request.get("evidence"))
execution_evidence_artifacts = as_dict(execution_evidence.get("artifacts"))
execution_guardrails = as_dict(execution_request.get("guardrails"))

rollout_runtime_inspect_path = resolve_ref(artifacts.get("rolloutRuntimeInspect"), evidence_path)
rollout_runtime_inspect = load_json(rollout_runtime_inspect_path)
rollout_runtime_target = as_dict(rollout_runtime_inspect.get("target"))
rollout_runtime_rollout = as_dict(rollout_runtime_inspect.get("rollout"))
rollout_runtime_analysis = as_dict(rollout_runtime_inspect.get("analysis"))
rollout_runtime_pods = as_dict(rollout_runtime_inspect.get("pods"))
rollout_runtime_guardrails = as_dict(rollout_runtime_inspect.get("guardrails"))

runtime_action_recommendation_path = resolve_ref(artifacts.get("runtimeActionRecommendation"), evidence_path)
runtime_action_recommendation = load_json(runtime_action_recommendation_path)
runtime_action_recommendation_target = as_dict(runtime_action_recommendation.get("target"))
runtime_action_recommendation_body = as_dict(runtime_action_recommendation.get("recommendation"))
runtime_action_recommendation_snapshot = as_dict(runtime_action_recommendation.get("runtimeSnapshot"))
runtime_action_recommendation_evidence_refs = as_dict(runtime_action_recommendation.get("evidenceRefs"))
runtime_action_recommendation_guardrails = as_dict(runtime_action_recommendation.get("guardrails"))

runtime_action_request_path = resolve_ref(artifacts.get("runtimeActionRequest"), evidence_path)
runtime_action_request = load_json(runtime_action_request_path)
runtime_action_request_target = as_dict(runtime_action_request.get("target"))
runtime_action_request_body = as_dict(runtime_action_request.get("request"))
runtime_action_request_binding = as_dict(runtime_action_request.get("recommendationBinding"))
runtime_action_request_snapshot = as_dict(runtime_action_request.get("runtimeSnapshot"))
runtime_action_request_approval = as_dict(runtime_action_request.get("approval"))
runtime_action_request_evidence_refs = as_dict(runtime_action_request.get("evidenceRefs"))
runtime_action_request_guardrails = as_dict(runtime_action_request.get("guardrails"))

runtime_action_preflight_path = resolve_ref(artifacts.get("runtimeActionPreflight"), evidence_path)
runtime_action_preflight = load_json(runtime_action_preflight_path)
runtime_action_preflight_target = as_dict(runtime_action_preflight.get("target"))
runtime_action_preflight_body = as_dict(runtime_action_preflight.get("request"))
runtime_action_preflight_decision = as_dict(runtime_action_preflight.get("preflight"))
runtime_action_preflight_snapshot = as_dict(runtime_action_preflight.get("runtimeSnapshot"))
runtime_action_preflight_evidence_refs = as_dict(runtime_action_preflight.get("evidenceRefs"))
runtime_action_preflight_guardrails = as_dict(runtime_action_preflight.get("guardrails"))

runtime_action_execution_result_path = resolve_ref(artifacts.get("runtimeActionExecutionResult"), evidence_path)
runtime_action_execution_result = load_json(runtime_action_execution_result_path)
runtime_action_execution_result_target = as_dict(runtime_action_execution_result.get("target"))
runtime_action_execution_result_action = as_dict(runtime_action_execution_result.get("action"))
runtime_action_execution_result_body = as_dict(runtime_action_execution_result.get("result"))
runtime_action_execution_result_executor = as_dict(runtime_action_execution_result.get("executor"))
runtime_action_execution_result_write_gate = as_dict(runtime_action_execution_result.get("writeGate"))
runtime_action_execution_result_before_snapshot = as_dict(runtime_action_execution_result.get("beforeSnapshot"))
runtime_action_execution_result_after_snapshot = as_dict(runtime_action_execution_result.get("afterSnapshot"))
runtime_action_execution_result_verification = as_dict(runtime_action_execution_result.get("postActionVerification"))
runtime_action_execution_result_rollback_target = as_dict(runtime_action_execution_result.get("rollbackTarget"))
runtime_action_execution_result_receipt = as_dict(runtime_action_execution_result.get("receipt"))
runtime_action_execution_result_evidence_refs = as_dict(runtime_action_execution_result.get("evidenceRefs"))
runtime_action_execution_result_guardrails = as_dict(runtime_action_execution_result.get("guardrails"))
runtime_action_execution_result_actor_boundary = as_dict(runtime_action_execution_result.get("actorBoundary"))
runtime_action_execution_result_recovery_boundary = as_dict(runtime_action_execution_result.get("recoveryBoundary"))
runtime_action_execution_result_execution_safety_boundary = as_dict(runtime_action_execution_result.get("executionSafetyBoundary"))
runtime_action_execution_result_actor_executor_identity = as_dict(runtime_action_execution_result_actor_boundary.get("executorIdentity"))
runtime_action_execution_result_actor_rbac_boundary = as_dict(runtime_action_execution_result_actor_boundary.get("rbacBoundary"))
runtime_action_execution_result_recovery_retry = as_dict(runtime_action_execution_result_recovery_boundary.get("retry"))
runtime_action_execution_result_recovery_failure = as_dict(runtime_action_execution_result_recovery_boundary.get("failureRecovery"))
runtime_action_execution_result_safety_default_policy = as_dict(runtime_action_execution_result_execution_safety_boundary.get("defaultPolicy"))
runtime_action_execution_result_safety_operation_risk = as_dict(runtime_action_execution_result_execution_safety_boundary.get("operationRisk"))
runtime_action_execution_result_safety_decision = as_dict(runtime_action_execution_result_execution_safety_boundary.get("safetyDecision"))

supply_chain_decision_path = resolve_ref(artifacts.get("supplyChainDecision"), evidence_path)
supply_chain_decision = load_json(supply_chain_decision_path)
supply_chain_decision_obj = as_dict(supply_chain_decision.get("decision"))
supply_chain_risk = as_dict(supply_chain_decision.get("risk"))
supply_chain_image = as_dict(supply_chain_decision.get("image"))
supply_chain_gitops = as_dict(supply_chain_decision.get("gitops"))
supply_chain_guardrails = as_dict(supply_chain_decision.get("guardrails"))

agent_trace_path = resolve_ref(artifacts.get("agentTrace"), evidence_path)
agent_trace = load_json(agent_trace_path)

otel_span_bundle_path = resolve_ref(artifacts.get("otelSpanBundle"), evidence_path)
otel_span_bundle = load_json(otel_span_bundle_path)
otel_source = as_dict(otel_span_bundle.get("source"))

trace_id = first_not_none(
    evidence.get("traceId"),
    agent_trace.get("traceId"),
    otel_span_bundle.get("traceId"),
)
agent_trace_id = first_not_none(
    evidence.get("agentTraceId"),
    agent_trace.get("agentTraceId"),
    otel_source.get("agentTraceId"),
)
root_span_id = first_not_none(
    evidence.get("rootSpanId"),
    otel_span_bundle.get("rootSpanId"),
)

link_map = {
    "releaseContext": artifacts.get("releaseContext"),
    "environmentConfig": artifacts.get("environmentConfig") or environment_config_ref,
    "releaseEvidence": str(evidence_path),
    "aiDecision": artifacts.get("aiDecision"),
    "policyDecision": artifacts.get("policyDecision"),
    "actionPlan": artifacts.get("actionPlan"),
    "approval": artifacts.get("approvalRecord") or artifacts.get("approval"),
    "timeline": artifacts.get("releaseTimeline") or artifacts.get("timeline"),
    "runbook": artifacts.get("runbook"),
    "rca": artifacts.get("rca"),
    "agentRun": artifacts.get("agentRun"),
    "agentTrace": artifacts.get("agentTrace"),
    "otelSpanBundle": artifacts.get("otelSpanBundle"),
    "planRun": artifacts.get("planRun"),
    "executionRequest": artifacts.get("executionRequest"),
    "rolloutRuntimeInspect": artifacts.get("rolloutRuntimeInspect"),
    "runtimeActionRecommendation": artifacts.get("runtimeActionRecommendation"),
    "runtimeActionRequest": artifacts.get("runtimeActionRequest"),
    "runtimeActionPreflight": artifacts.get("runtimeActionPreflight"),
    "runtimeActionExecutionResult": artifacts.get("runtimeActionExecutionResult"),
    "supplyChainDecision": artifacts.get("supplyChainDecision"),
}

artifact_defs = [
    ("releaseContext", link_map["releaseContext"], True),
    ("environmentConfig", link_map["environmentConfig"], False),
    ("releaseEvidence", link_map["releaseEvidence"], True),
    ("releaseReport", artifacts.get("releaseReport"), False),
    ("aiAdvice", artifacts.get("aiAdvice"), False),
    ("aiDecision", link_map["aiDecision"], True),
    ("policyDecision", link_map["policyDecision"], True),
    ("releaseSummary", artifacts.get("releaseSummary"), False),
    ("failureEvidence", artifacts.get("failureEvidence"), False),
    ("failureEvidenceReport", artifacts.get("failureEvidenceReport"), False),
    ("actionPlan", link_map["actionPlan"], False),
    ("actionPlanReport", artifacts.get("actionPlanReport"), False),
    ("releaseIntelligence", artifacts.get("releaseIntelligence"), False),
    ("releaseIntelligenceReport", artifacts.get("releaseIntelligenceReport"), False),
    ("agentRun", link_map["agentRun"], False),
    ("agentTrace", link_map["agentTrace"], False),
    ("otelSpanBundle", link_map["otelSpanBundle"], False),
    ("planRun", link_map["planRun"], False),
    ("executionRequest", link_map["executionRequest"], False),
    ("rolloutRuntimeInspect", link_map["rolloutRuntimeInspect"], False),
    ("runtimeActionRecommendation", link_map["runtimeActionRecommendation"], False),
    ("runtimeActionRequest", link_map["runtimeActionRequest"], False),
    ("runtimeActionPreflight", link_map["runtimeActionPreflight"], False),
    ("runtimeActionExecutionResult", link_map["runtimeActionExecutionResult"], False),
    ("approval", link_map["approval"], False),
    ("timeline", link_map["timeline"], False),
    ("runbook", link_map["runbook"], False),
    ("rca", link_map["rca"], False),
]

if link_map.get("supplyChainDecision"):
    artifact_defs.append(("supplyChainDecision", link_map["supplyChainDecision"], False))

artifact_records = {
    kind: artifact_entry(kind, ref, evidence_path, required)
    for kind, ref, required in artifact_defs
}

total = len(artifact_records)
collected = sum(1 for item in artifact_records.values() if item.get("exists"))
missing = [kind for kind, item in artifact_records.items() if not item.get("exists")]

safe_service = scalar(service, "unknown").replace("/", "-").replace(" ", "-")
safe_env = scalar(env, "unknown").replace("/", "-").replace(" ", "-")
evidence_id = f"ev-{release_id}-{safe_service}-{safe_env}"

record = {
    "schemaVersion": "evidence.record/v1alpha1",
    "generatedBy": "build-evidence-record.sh",
    "generatedAt": now(),
    "evidenceId": evidence_id,
    "releaseId": release_id,
    "traceId": nullable_string(trace_id),
    "agentTraceId": nullable_string(agent_trace_id),
    "rootSpanId": nullable_string(root_span_id),
    "service": service,
    "namespace": namespace,
    "env": env,
    "environmentConfigRef": nullable_string(environment_config_ref),
    "observability": {
        "traceId": nullable_string(trace_id),
        "agentTraceId": nullable_string(agent_trace_id),
        "rootSpanId": nullable_string(root_span_id),
        "agentTrace": str(agent_trace_path) if agent_trace_path else None,
        "otelSpanBundle": str(otel_span_bundle_path) if otel_span_bundle_path else None,
        "localFileOnly": True,
        "doesNotSendExternalTelemetry": True,
        "doesNotCallExternalCollector": True,
    },
    "environmentProfile": nullable_string(environment_profile),
    "clusterName": nullable_string(cluster_name),
    "environmentClass": nullable_string(environment_class),
    "policyProfile": nullable_string(policy_profile),
    "gitopsOverlayPath": nullable_string(gitops_overlay_path),
    "version": nullable_string(version),
    "commit": nullable_string(commit),
    "image": nullable_string(image),
    "imageDigest": nullable_string(image_digest),
    "sourceEvidence": str(evidence_path),
    "releaseResult": scalar(evidence.get("releaseResult")),
    "policyDecision": scalar(evidence.get("policyDecision")),
    "finalAction": scalar(evidence.get("finalAction")),
    "executionMode": nullable_string(evidence.get("executionMode")),
    "requiresHumanApproval": bool(evidence.get("requiresHumanApproval", False)),
    "environment": {
        "env": nullable_string(env),
        "profile": nullable_string(environment_profile),
        "clusterName": nullable_string(cluster_name),
        "environmentClass": nullable_string(environment_class),
        "namespace": nullable_string(namespace),
        "policyProfile": nullable_string(policy_profile),
        "gitopsOverlayPath": nullable_string(gitops_overlay_path),
        "configRef": nullable_string(environment_config_ref),
        "configCaptured": isinstance(environment_config_snapshot, dict),
    },
    "policy": {
        "policyDecisionId": nullable_string(first_not_none(
            evidence.get("policyDecisionId"),
            policy_decision_ref.get("policyDecisionId"),
        )),
        "requestedAction": nullable_string(first_not_none(
            evidence.get("requestedAction"),
            policy_decision_ref.get("requestedAction"),
        )),
        "allowed": bool_or_none(first_not_none(
            evidence.get("allowed"),
            policy_decision_ref.get("allowed"),
        )),
        "deniedReasons": [str(item) for item in as_list(first_not_none(
            evidence.get("deniedReasons"),
            policy_decision_ref.get("deniedReasons"),
        ))],
        "approvalRequiredReasons": [str(item) for item in as_list(first_not_none(
            evidence.get("approvalRequiredReasons"),
            policy_decision_ref.get("approvalRequiredReasons"),
        ))],
        "matchedRules": [str(item) for item in as_list(first_not_none(
            summary.get("matchedPolicyRules"),
            policy_decision_ref.get("matchedRules"),
        ))],
        "strategyPolicy": as_dict(first_not_none(
            evidence.get("strategyPolicy"),
            policy_decision_ref.get("strategyPolicy"),
        )),
        "safety": as_dict(first_not_none(
            evidence.get("policySafety"),
            policy_decision_ref.get("safety"),
        )),
    },
    "agent": {
        "agentRunId": nullable_string(agent_run.get("agentRunId")),
        "mode": nullable_string(agent_run.get("mode")),
        "recommendedAction": nullable_string(agent_recommendation.get("recommendedAction")),
        "priority": nullable_string(agent_recommendation.get("priority")),
        "willExecute": bool_or_none(first_not_none(
            agent_recommendation.get("willExecute"),
            agent_guardrails.get("willExecute"),
        )),
        "sourceAgentRun": nullable_string(link_map.get("agentRun")),
        "guardrails": agent_guardrails,
    },
    "plan": {
        "planRunId": nullable_string(plan_run.get("planRunId")),
        "mode": nullable_string(plan_run.get("mode")),
        "sourceAgentRunId": nullable_string(plan_run.get("sourceAgentRunId")),
        "planType": nullable_string(plan_obj.get("planType")),
        "priority": nullable_string(plan_obj.get("priority")),
        "willExecute": bool_or_none(first_not_none(
            plan_obj.get("willExecute"),
            plan_guardrails.get("willExecute"),
        )),
        "sourcePlanRun": nullable_string(link_map.get("planRun")),
        "retrievedEvidenceCount": plan_retrieval_summary.get("retrievedEvidenceCount"),
        "topScore": plan_retrieval_summary.get("topScore"),
        "guardrails": plan_guardrails,
    },
    "executionRequest": {
        "executionRequestId": nullable_string(execution_request.get("executionRequestId")),
        "mode": nullable_string(execution_request.get("mode")),
        "sourcePlanRunId": nullable_string(execution_request.get("sourcePlanRunId")),
        "requestedAction": nullable_string(execution_request_body.get("requestedAction")),
        "requestStatus": nullable_string(execution_request_body.get("requestStatus")),
        "lifecycleStage": nullable_string(execution_request_body.get("lifecycleStage")),
        "requestedBy": nullable_string(execution_request_body.get("requestedBy")),
        "policyDecision": nullable_string(execution_policy_binding.get("policyDecision")),
        "requiresHumanApproval": bool_or_none(execution_policy_binding.get("requiresHumanApproval")),
        "approvalStatus": nullable_string(execution_approval.get("status")),
        "approved": bool_or_none(execution_approval.get("approved")),
        "approvalDecision": nullable_string(execution_approval.get("approvalDecision")),
        "approvalReason": nullable_string(execution_approval.get("reason")),
        "approver": nullable_string(execution_approval.get("approver")),
        "readyToExecute": bool_or_none(execution_approval.get("readyToExecute")),
        "willExecute": bool_or_none(first_not_none(
            execution_request_body.get("willExecute"),
            execution_policy_binding.get("willExecute"),
            execution_guardrails.get("willExecute"),
        )),
        "sourceExecutionRequest": nullable_string(link_map.get("executionRequest")),
        "approvalRecord": nullable_string(first_not_none(
            execution_evidence.get("approvalRecord"),
            execution_evidence_artifacts.get("approvalRecord"),
        )),
        "approvalRecordReport": nullable_string(first_not_none(
            execution_evidence.get("approvalRecordReport"),
            execution_evidence_artifacts.get("approvalRecordReport"),
        )),
        "guardrails": execution_guardrails,
    },
    "rolloutRuntimeInspect": {
        "rolloutRuntimeInspectId": nullable_string(rollout_runtime_inspect.get("rolloutRuntimeInspectId")),
        "mode": nullable_string(rollout_runtime_inspect.get("mode")),
        "rolloutName": nullable_string(first_not_none(
            rollout_runtime_target.get("rolloutName"),
            rollout_runtime_rollout.get("name"),
        )),
        "namespace": nullable_string(first_not_none(
            rollout_runtime_target.get("namespace"),
            rollout_runtime_rollout.get("namespace"),
        )),
        "service": nullable_string(first_not_none(
            rollout_runtime_target.get("service"),
            service,
        )),
        "env": nullable_string(first_not_none(
            rollout_runtime_target.get("env"),
            env,
        )),
        "rolloutPhase": nullable_string(rollout_runtime_rollout.get("phase")),
        "strategy": nullable_string(rollout_runtime_rollout.get("strategy")),
        "currentStepIndex": rollout_runtime_rollout.get("currentStepIndex"),
        "replicas": rollout_runtime_rollout.get("replicas"),
        "updatedReplicas": rollout_runtime_rollout.get("updatedReplicas"),
        "readyReplicas": rollout_runtime_rollout.get("readyReplicas"),
        "availableReplicas": rollout_runtime_rollout.get("availableReplicas"),
        "paused": bool_or_none(rollout_runtime_rollout.get("paused")),
        "degraded": bool_or_none(rollout_runtime_rollout.get("degraded")),
        "analysisRunName": nullable_string(rollout_runtime_analysis.get("analysisRunName")),
        "analysisStatus": nullable_string(rollout_runtime_analysis.get("status")),
        "podCount": rollout_runtime_pods.get("podCount"),
        "readyPodCount": rollout_runtime_pods.get("readyPodCount"),
        "runningPodCount": rollout_runtime_pods.get("runningPodCount"),
        "sourceRolloutRuntimeInspect": nullable_string(link_map.get("rolloutRuntimeInspect")),
        "guardrails": rollout_runtime_guardrails,
    },
    "runtimeActionRecommendation": {
        "runtimeActionRecommendationId": nullable_string(runtime_action_recommendation.get("runtimeActionRecommendationId")),
        "mode": nullable_string(runtime_action_recommendation.get("mode")),
        "recommendationStatus": nullable_string(runtime_action_recommendation_body.get("recommendationStatus")),
        "recommendedAction": nullable_string(runtime_action_recommendation_body.get("recommendedAction")),
        "riskLevel": nullable_string(runtime_action_recommendation_body.get("riskLevel")),
        "confidence": nullable_string(runtime_action_recommendation_body.get("confidence")),
        "approvalRequired": bool_or_none(runtime_action_recommendation_body.get("approvalRequired")),
        "reasons": [str(item) for item in as_list(runtime_action_recommendation_body.get("reasons"))],
        "summary": nullable_string(runtime_action_recommendation_body.get("summary")),
        "rolloutName": nullable_string(runtime_action_recommendation_target.get("rolloutName")),
        "namespace": nullable_string(runtime_action_recommendation_target.get("namespace")),
        "service": nullable_string(first_not_none(runtime_action_recommendation_target.get("service"), service)),
        "env": nullable_string(first_not_none(runtime_action_recommendation_target.get("env"), env)),
        "rolloutPhase": nullable_string(runtime_action_recommendation_snapshot.get("rolloutPhase")),
        "analysisStatus": nullable_string(runtime_action_recommendation_snapshot.get("analysisStatus")),
        "sourceRolloutRuntimeInspectId": nullable_string(runtime_action_recommendation_evidence_refs.get("sourceRolloutRuntimeInspectId")),
        "sourceRolloutRuntimeInspect": nullable_string(runtime_action_recommendation_evidence_refs.get("rolloutRuntimeInspect")),
        "sourceRuntimeActionRecommendation": nullable_string(link_map.get("runtimeActionRecommendation")),
        "guardrails": runtime_action_recommendation_guardrails,
    },
    "runtimeActionRequest": {
        "runtimeActionRequestId": nullable_string(runtime_action_request.get("runtimeActionRequestId")),
        "mode": nullable_string(runtime_action_request.get("mode")),
        "sourceRuntimeActionRecommendationId": nullable_string(runtime_action_request.get("sourceRuntimeActionRecommendationId")),
        "requestedAction": nullable_string(runtime_action_request_body.get("requestedAction")),
        "requestStatus": nullable_string(runtime_action_request_body.get("requestStatus")),
        "lifecycleStage": nullable_string(runtime_action_request_body.get("lifecycleStage")),
        "requestedBy": nullable_string(runtime_action_request_body.get("requestedBy")),
        "riskLevel": nullable_string(runtime_action_request_body.get("riskLevel")),
        "confidence": nullable_string(runtime_action_request_body.get("confidence")),
        "approvalRequired": bool_or_none(runtime_action_request_body.get("approvalRequired")),
        "readyToExecute": bool_or_none(runtime_action_request_body.get("readyToExecute")),
        "willExecute": bool_or_none(first_not_none(
            runtime_action_request_body.get("willExecute"),
            runtime_action_request_binding.get("willExecute"),
            runtime_action_request_guardrails.get("willExecute"),
        )),
        "recommendationStatus": nullable_string(runtime_action_request_binding.get("recommendationStatus")),
        "recommendedAction": nullable_string(runtime_action_request_binding.get("recommendedAction")),
        "allowedToRequest": bool_or_none(runtime_action_request_binding.get("allowedToRequest")),
        "blockingReasons": [str(item) for item in as_list(runtime_action_request_binding.get("blockingReasons"))],
        "approvalStatus": nullable_string(runtime_action_request_approval.get("status")),
        "approved": bool_or_none(runtime_action_request_approval.get("approved")),
        "approvalDecision": nullable_string(runtime_action_request_approval.get("approvalDecision")),
        "rolloutName": nullable_string(runtime_action_request_target.get("rolloutName")),
        "namespace": nullable_string(runtime_action_request_target.get("namespace")),
        "service": nullable_string(first_not_none(runtime_action_request_target.get("service"), service)),
        "env": nullable_string(first_not_none(runtime_action_request_target.get("env"), env)),
        "rolloutPhase": nullable_string(runtime_action_request_snapshot.get("rolloutPhase")),
        "analysisStatus": nullable_string(runtime_action_request_snapshot.get("analysisStatus")),
        "sourceRuntimeActionRecommendation": nullable_string(runtime_action_request_evidence_refs.get("runtimeActionRecommendation")),
        "sourceRolloutRuntimeInspect": nullable_string(runtime_action_request_evidence_refs.get("rolloutRuntimeInspect")),
        "sourceRolloutRuntimeInspectId": nullable_string(runtime_action_request_evidence_refs.get("sourceRolloutRuntimeInspectId")),
        "sourceRuntimeActionRequest": nullable_string(link_map.get("runtimeActionRequest")),
        "guardrails": runtime_action_request_guardrails,
    },
    "runtimeActionPreflight": {
        "runtimeActionPreflightId": nullable_string(runtime_action_preflight.get("runtimeActionPreflightId")),
        "mode": nullable_string(runtime_action_preflight.get("mode")),
        "sourceRuntimeActionRequestId": nullable_string(runtime_action_preflight.get("sourceRuntimeActionRequestId")),
        "requestedAction": nullable_string(runtime_action_preflight_body.get("requestedAction")),
        "requestStatus": nullable_string(runtime_action_preflight_body.get("requestStatus")),
        "lifecycleStage": nullable_string(runtime_action_preflight_body.get("lifecycleStage")),
        "riskLevel": nullable_string(runtime_action_preflight_body.get("riskLevel")),
        "confidence": nullable_string(runtime_action_preflight_body.get("confidence")),
        "approvalRequired": bool_or_none(runtime_action_preflight_body.get("approvalRequired")),
        "approved": bool_or_none(runtime_action_preflight_body.get("approved")),
        "allowedToRequest": bool_or_none(runtime_action_preflight_body.get("allowedToRequest")),
        "preflightStatus": nullable_string(runtime_action_preflight_decision.get("preflightStatus")),
        "eligibilityStatus": nullable_string(runtime_action_preflight_decision.get("eligibilityStatus")),
        "blockingReasons": [str(item) for item in as_list(runtime_action_preflight_decision.get("blockingReasons"))],
        "approvalReasons": [str(item) for item in as_list(runtime_action_preflight_decision.get("approvalReasons"))],
        "warningReasons": [str(item) for item in as_list(runtime_action_preflight_decision.get("warningReasons"))],
        "eligibleForExecution": bool_or_none(runtime_action_preflight_decision.get("eligibleForExecution")),
        "readyToExecute": bool_or_none(runtime_action_preflight_decision.get("readyToExecute")),
        "willExecute": bool_or_none(first_not_none(
            runtime_action_preflight_decision.get("willExecute"),
            runtime_action_preflight_body.get("willExecute"),
            runtime_action_preflight_guardrails.get("willExecute"),
        )),
        "rolloutName": nullable_string(runtime_action_preflight_target.get("rolloutName")),
        "namespace": nullable_string(runtime_action_preflight_target.get("namespace")),
        "service": nullable_string(first_not_none(runtime_action_preflight_target.get("service"), service)),
        "env": nullable_string(first_not_none(runtime_action_preflight_target.get("env"), env)),
        "rolloutPhase": nullable_string(runtime_action_preflight_snapshot.get("rolloutPhase")),
        "analysisStatus": nullable_string(runtime_action_preflight_snapshot.get("analysisStatus")),
        "sourceRuntimeActionRequest": nullable_string(runtime_action_preflight_evidence_refs.get("runtimeActionRequest")),
        "sourceRuntimeActionRecommendation": nullable_string(runtime_action_preflight_evidence_refs.get("runtimeActionRecommendation")),
        "sourceRuntimeActionRecommendationId": nullable_string(runtime_action_preflight_evidence_refs.get("sourceRuntimeActionRecommendationId")),
        "sourceRolloutRuntimeInspect": nullable_string(runtime_action_preflight_evidence_refs.get("rolloutRuntimeInspect")),
        "sourceRolloutRuntimeInspectId": nullable_string(runtime_action_preflight_evidence_refs.get("sourceRolloutRuntimeInspectId")),
        "sourceRuntimeActionPreflight": nullable_string(link_map.get("runtimeActionPreflight")),
        "guardrails": runtime_action_preflight_guardrails,
    },
    "runtimeActionExecutionResult": {
        "runtimeActionExecutionResultId": nullable_string(runtime_action_execution_result.get("runtimeActionExecutionResultId")),
        "mode": nullable_string(runtime_action_execution_result.get("mode")),
        "sourceRuntimeActionPreflightId": nullable_string(runtime_action_execution_result.get("sourceRuntimeActionPreflightId")),
        "sourceRuntimeActionRequestId": nullable_string(runtime_action_execution_result.get("sourceRuntimeActionRequestId")),
        "requestedAction": nullable_string(first_not_none(
            runtime_action_execution_result_action.get("requestedAction"),
            runtime_action_execution_result_body.get("requestedAction"),
        )),
        "actionStatus": nullable_string(first_not_none(
            runtime_action_execution_result_action.get("actionStatus"),
            runtime_action_execution_result_body.get("actionStatus"),
        )),
        "executionStatus": nullable_string(runtime_action_execution_result_body.get("executionStatus")),
        "verificationStatus": nullable_string(first_not_none(
            runtime_action_execution_result_verification.get("verificationStatus"),
            runtime_action_execution_result_body.get("verificationStatus"),
            runtime_action_execution_result_receipt.get("verificationStatus"),
        )),
        "pauseVerified": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("pauseVerified"),
            runtime_action_execution_result_body.get("pauseVerified"),
            runtime_action_execution_result_receipt.get("pauseVerified"),
        )),
        "resumeVerified": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("resumeVerified"),
            runtime_action_execution_result_body.get("resumeVerified"),
            runtime_action_execution_result_receipt.get("resumeVerified"),
        )),
        "promoteVerified": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("promoteVerified"),
            runtime_action_execution_result_body.get("promoteVerified"),
            runtime_action_execution_result_receipt.get("promoteVerified"),
        )),
        "abortVerified": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("abortVerified"),
            runtime_action_execution_result_body.get("abortVerified"),
            runtime_action_execution_result_receipt.get("abortVerified"),
        )),
        "rollbackVerified": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("rollbackVerified"),
            runtime_action_execution_result_body.get("rollbackVerified"),
            runtime_action_execution_result_receipt.get("rollbackVerified"),
        )),
        "postActionObserved": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("postActionObserved"),
            runtime_action_execution_result_body.get("postActionObserved"),
        )),
        "desiredStateObserved": bool_or_none(first_not_none(
            runtime_action_execution_result_verification.get("desiredStateObserved"),
            runtime_action_execution_result_body.get("desiredStateObserved"),
        )),
        "afterObservationMode": nullable_string(runtime_action_execution_result_after_snapshot.get("observationMode")),
        "commandMode": nullable_string(runtime_action_execution_result_action.get("commandMode")),
        "rollbackTarget": runtime_action_execution_result_rollback_target,
        "commandExitCode": runtime_action_execution_result_action.get("commandExitCode"),
        "commandWillExecute": bool_or_none(runtime_action_execution_result_action.get("commandWillExecute")),
        "didPause": bool_or_none(runtime_action_execution_result_body.get("didPause")),
        "didResume": bool_or_none(runtime_action_execution_result_body.get("didResume")),
        "didPromote": bool_or_none(runtime_action_execution_result_body.get("didPromote")),
        "didAbort": bool_or_none(runtime_action_execution_result_body.get("didAbort")),
        "didRollback": bool_or_none(runtime_action_execution_result_body.get("didRollback")),
        "attemptedKubernetesMutation": bool_or_none(runtime_action_execution_result_body.get("attemptedKubernetesMutation")),
        "mutatedKubernetes": bool_or_none(runtime_action_execution_result_body.get("mutatedKubernetes")),
        "mutatedGitOps": bool_or_none(runtime_action_execution_result_body.get("mutatedGitOps")),
        "didModifyKubernetes": bool_or_none(runtime_action_execution_result_receipt.get("didModifyKubernetes")),
        "didModifyGitOps": bool_or_none(runtime_action_execution_result_receipt.get("didModifyGitOps")),
        "executorName": nullable_string(runtime_action_execution_result_executor.get("executorName")),
        "executorAdapter": nullable_string(runtime_action_execution_result_executor.get("adapter")),
        "preflightStatus": nullable_string(runtime_action_execution_result_write_gate.get("preflightStatus")),
        "eligibilityStatus": nullable_string(runtime_action_execution_result_write_gate.get("eligibilityStatus")),
        "finalExecuteEnabled": bool_or_none(runtime_action_execution_result_write_gate.get("finalExecuteEnabled")),
        "writeAllowed": bool_or_none(runtime_action_execution_result_write_gate.get("writeAllowed")),
        "rolloutName": nullable_string(runtime_action_execution_result_target.get("rolloutName")),
        "namespace": nullable_string(runtime_action_execution_result_target.get("namespace")),
        "service": nullable_string(first_not_none(runtime_action_execution_result_target.get("service"), service)),
        "env": nullable_string(first_not_none(runtime_action_execution_result_target.get("env"), env)),
        "rolloutPhase": nullable_string(runtime_action_execution_result_before_snapshot.get("rolloutPhase")),
        "analysisStatus": nullable_string(runtime_action_execution_result_before_snapshot.get("analysisStatus")),
        "sourceRuntimeActionPreflight": nullable_string(runtime_action_execution_result_evidence_refs.get("runtimeActionPreflight")),
        "sourceRuntimeActionRequest": nullable_string(runtime_action_execution_result_evidence_refs.get("runtimeActionRequest")),
        "sourceRuntimeActionRecommendation": nullable_string(runtime_action_execution_result_evidence_refs.get("runtimeActionRecommendation")),
        "sourceRolloutRuntimeInspect": nullable_string(runtime_action_execution_result_evidence_refs.get("rolloutRuntimeInspect")),
        "sourceRolloutRuntimeInspectId": nullable_string(runtime_action_execution_result_evidence_refs.get("sourceRolloutRuntimeInspectId")),
        "sourceRuntimeActionExecutionResult": nullable_string(link_map.get("runtimeActionExecutionResult")),
        "guardrails": runtime_action_execution_result_guardrails,
        "actorBoundary": runtime_action_execution_result_actor_boundary,
        "recoveryBoundary": runtime_action_execution_result_recovery_boundary,
        "executionSafetyBoundary": runtime_action_execution_result_execution_safety_boundary,
        "actorRuntimeIdentity": nullable_string(runtime_action_execution_result_actor_executor_identity.get("runtimeIdentity")),
        "actorKubernetesIdentity": nullable_string(runtime_action_execution_result_actor_executor_identity.get("kubernetesIdentity")),
        "watcherRbacMode": nullable_string(runtime_action_execution_result_actor_rbac_boundary.get("watcherRbacMode")),
        "watcherCanMutateKubernetes": bool_or_none(runtime_action_execution_result_actor_rbac_boundary.get("watcherCanMutateKubernetes")),
        "manualRetryAllowed": bool_or_none(runtime_action_execution_result_recovery_retry.get("manualRetryAllowed")),
        "recoveryFailureMode": nullable_string(runtime_action_execution_result_recovery_failure.get("failureMode")),
        "executionDefaultOff": bool_or_none(runtime_action_execution_result_safety_default_policy.get("defaultOff")),
        "executionSafetyRiskLevel": nullable_string(runtime_action_execution_result_safety_operation_risk.get("riskLevel")),
        "executionDirectExecutionAllowed": bool_or_none(runtime_action_execution_result_safety_decision.get("directExecutionAllowed")),
        "executionSafetyBlockingReasons": as_list(runtime_action_execution_result_safety_decision.get("blockingReasons")),
    },
    "supplyChain": {
        "supplyChainDecisionId": nullable_string(supply_chain_decision.get("supplyChainDecisionId")),
        "mode": nullable_string(supply_chain_decision.get("mode")),
        "decision": nullable_string(supply_chain_decision_obj.get("decision")),
        "allowed": bool_or_none(supply_chain_decision_obj.get("allowed")),
        "requiresHumanApproval": bool_or_none(supply_chain_decision_obj.get("requiresHumanApproval")),
        "riskLevel": nullable_string(supply_chain_risk.get("riskLevel")),
        "riskScore": supply_chain_risk.get("riskScore"),
        "image": nullable_string(supply_chain_image.get("image")),
        "imageTag": nullable_string(supply_chain_image.get("imageTag")),
        "imageDigest": nullable_string(supply_chain_image.get("imageDigest")),
        "usesMutableTag": bool_or_none(supply_chain_image.get("usesMutableTag")),
        "gitopsManifest": nullable_string(supply_chain_gitops.get("manifest")),
        "gitopsManifestFound": bool_or_none(supply_chain_gitops.get("manifestFound")),
        "gitopsReleaseTag": nullable_string(first_not_none(
            supply_chain_gitops.get("releaseTag"),
            supply_chain_gitops.get("imageTag"),
        )),
        "checkCount": len(supply_chain_decision.get("checks") or []),
        "blockingReasons": [str(item) for item in as_list(supply_chain_decision_obj.get("blockingReasons"))],
        "warningReasons": [str(item) for item in as_list(supply_chain_decision_obj.get("warningReasons"))],
        "willExecute": bool_or_none(supply_chain_guardrails.get("willExecute")),
        "sourceSupplyChainDecision": nullable_string(link_map.get("supplyChainDecision")),
        "guardrails": supply_chain_guardrails,
    },
    "slo": {
        "sloId": slo_id,
        "sloConfigRef": slo_config_ref,
        "snapshotCaptured": isinstance(slo_snapshot, dict),
        "objectiveIds": objective_ids(slo_snapshot),
    },
    "strategy": {
        "strategyId": strategy_id,
        "strategyConfigRef": strategy_config_ref,
        "snapshotCaptured": isinstance(strategy_snapshot, dict),
        "strategyType": strategy_spec_value(strategy_snapshot, "strategyType"),
        "trafficSteps": strategy_step_summaries(strategy_snapshot),
        "failurePolicy": strategy_failure_policy if isinstance(strategy_failure_policy, dict) else {},
        "promotionPolicy": strategy_promotion_policy if isinstance(strategy_promotion_policy, dict) else {},
    },
    "summary": summary,
    "artifacts": artifact_records,
    "links": {
        key: nullable_string(value)
        for key, value in link_map.items()
    },
    "decisionRefs": decision_refs,
    "coverage": {
        "total": total,
        "collected": collected,
        "missing": missing,
    },
    "safety": {
        "readOnly": True,
        "willExecute": False,
        "supportsRollback": False,
        "supportsPromote": False,
        "supportsPatch": False,
        "supportsDelete": False,
    },
}

output_json.write_text(json.dumps(record, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
shutil.copyfile(output_json, latest_json)

print(f"Evidence record generated: {output_json}")
print(f"Latest evidence record: {latest_json}")
print(json.dumps({
    "evidenceId": evidence_id,
    "releaseId": release_id,
    "service": service,
    "env": env,
    "releaseResult": record["releaseResult"],
    "policyDecision": record["policyDecision"],
    "collected": collected,
    "total": total,
    "missing": missing,
}, indent=2, ensure_ascii=False))
PY
