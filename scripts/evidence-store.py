#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


RESOURCE_SPECS = [
    {
        "object_type": "releaseEvidence",
        "glob": "release-evidence-*.json",
        "latest": "release-evidence-latest.json",
        "prefix": "release-evidence-",
        "id_key": None,
        "id_prefix": "re-",
    },
    {
        "object_type": "evidenceRecord",
        "glob": "evidence-record-*.json",
        "latest": "evidence-record-latest.json",
        "prefix": "evidence-record-",
        "id_key": "evidenceId",
        "id_prefix": "ev-",
    },
    {
        "object_type": "agentRun",
        "glob": "agent-run-*.json",
        "latest": "agent-run-latest.json",
        "prefix": "agent-run-",
        "id_key": "agentRunId",
        "id_prefix": "ar-",
    },
    {
        "object_type": "agentTrace",
        "schema_version": "agent.trace/v1alpha1",
        "glob": "agent-trace-*.json",
        "latest": "agent-trace-latest.json",
        "prefix": "agent-trace-",
        "id_key": "agentTraceId",
        "id_prefix": "at-",
    },
    {
        "object_type": "otelSpanBundle",
        "schema_version": "otel.span.bundle/v1alpha1",
        "glob": "otel-span-bundle-*.json",
        "latest": "otel-span-bundle-latest.json",
        "prefix": "otel-span-bundle-",
        "id_key": "traceId",
        "id_prefix": "trace-",
    },
    {
        "object_type": "planRun",
        "glob": "plan-run-*.json",
        "latest": "plan-run-latest.json",
        "prefix": "plan-run-",
        "id_key": "planRunId",
        "id_prefix": "pr-",
    },
    {
        "object_type": "rolloutRuntimeInspect",
        "glob": "rollout-runtime-inspect-*.json",
        "latest": "rollout-runtime-inspect-latest.json",
        "prefix": "rollout-runtime-inspect-",
        "id_key": "rolloutRuntimeInspectId",
        "id_prefix": "rti-",
    },
    {
        "object_type": "runtimeActionRecommendation",
        "glob": "runtime-action-recommendation-*.json",
        "latest": "runtime-action-recommendation-latest.json",
        "prefix": "runtime-action-recommendation-",
        "id_key": "runtimeActionRecommendationId",
        "id_prefix": "rar-",
    },
    {
        "object_type": "runtimeActionRequest",
        "glob": "runtime-action-request-*.json",
        "latest": "runtime-action-request-latest.json",
        "prefix": "runtime-action-request-",
        "id_key": "runtimeActionRequestId",
        "id_prefix": "rarq-",
    },
    {
        "object_type": "runtimeActionPreflight",
        "glob": "runtime-action-preflight-*.json",
        "latest": "runtime-action-preflight-latest.json",
        "prefix": "runtime-action-preflight-",
        "id_key": "runtimeActionPreflightId",
        "id_prefix": "rap-",
    },
    {
        "object_type": "runtimeActionExecutionResult",
        "glob": "runtime-action-execution-result-*.json",
        "latest": "runtime-action-execution-result-latest.json",
        "prefix": "runtime-action-execution-result-",
        "id_key": "runtimeActionExecutionResultId",
        "id_prefix": "raer-",
    },
    {
        "object_type": "executionRequest",
        "glob": "execution-request-*.json",
        "latest": "execution-request-latest.json",
        "prefix": "execution-request-",
        "id_key": "executionRequestId",
        "id_prefix": "er-",
    },
    {
        "object_type": "policyInput",
        "schema_version": "policy.input/v1alpha1",
        "prefix": "policy-input-",
        "latest": "policy-input-latest.json",
        "glob": "policy-input-*.json",
        "id_prefix": "pi-",
    },
    {
        "object_type": "policyRuntimeResult",
        "schema_version": "policy.runtime.result/v1alpha1",
        "prefix": "policy-runtime-result-",
        "latest": "policy-runtime-result-latest.json",
        "glob": "policy-runtime-result-*.json",
        "id_prefix": "prr-",
    },
    {
        "object_type": "policyDecision",
        "schema_version": "release.policy.evaluator/v1alpha1",
        "prefix": "policy-decision-",
        "latest": "policy-decision-latest.json",
        "glob": "policy-decision-*.json",
        "id_key": "policyDecisionId",
        "id_prefix": "pd-",
    },
    {
        "object_type": "signedReleaseGate",
        "schema_version": "signed.release.gate/v1alpha1",
        "glob": "signed-release-gate-*.json",
        "latest": "signed-release-gate-latest.json",
        "prefix": "signed-release-gate-",
        "id_key": "signedReleaseGateId",
        "id_prefix": "srg-",
    },
    {
        "object_type": "supplyChainDecision",
        "glob": "supply-chain-decision-*.json",
        "latest": "supply-chain-decision-latest.json",
        "prefix": "supply-chain-decision-",
        "id_key": "supplyChainDecisionId",
        "id_prefix": "sc-",
    },
]


CURRENT_DB_SCHEMA_VERSION = 1
CURRENT_DB_SCHEMA_ID = "evidence.store.sqlite/v1alpha1"
SCHEMA_MIGRATIONS = [
    {
        "migrationId": "001_initial_evidence_store",
        "schemaVersion": CURRENT_DB_SCHEMA_ID,
        "version": CURRENT_DB_SCHEMA_VERSION,
        "description": "Create initial EvidenceStore release, object, artifact, and schema metadata tables.",
    },
]


SCHEMA_SQL = """
PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS releases (
  release_id TEXT PRIMARY KEY,
  service TEXT,
  namespace TEXT,
  env TEXT,
  version TEXT,
  commit_sha TEXT,
  image TEXT,
  image_digest TEXT,
  release_result TEXT,
  policy_decision TEXT,
  final_action TEXT,
  risk_level TEXT,
  risk_score REAL,
  requires_human_approval INTEGER,
  generated_at TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS evidence_objects (
  object_pk TEXT PRIMARY KEY,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  release_id TEXT NOT NULL,
  schema_version TEXT,
  source_path TEXT NOT NULL,
  source_mtime TEXT,
  content_sha256 TEXT NOT NULL,
  generated_at TEXT,
  imported_at TEXT NOT NULL,
  summary_json TEXT NOT NULL,
  raw_json TEXT NOT NULL,
  FOREIGN KEY (release_id) REFERENCES releases(release_id)
);

CREATE INDEX IF NOT EXISTS idx_evidence_objects_release
  ON evidence_objects(release_id);

CREATE INDEX IF NOT EXISTS idx_evidence_objects_type_id
  ON evidence_objects(object_type, object_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_evidence_objects_source
  ON evidence_objects(source_path);

CREATE TABLE IF NOT EXISTS release_artifacts (
  release_id TEXT NOT NULL,
  artifact_kind TEXT NOT NULL,
  path TEXT NOT NULL,
  exists_flag INTEGER,
  content_type TEXT,
  size_bytes INTEGER,
  modified_at TEXT,
  source_object_pk TEXT,
  PRIMARY KEY (release_id, artifact_kind, path),
  FOREIGN KEY (release_id) REFERENCES releases(release_id),
  FOREIGN KEY (source_object_pk) REFERENCES evidence_objects(object_pk)
);

CREATE INDEX IF NOT EXISTS idx_release_artifacts_release
  ON release_artifacts(release_id);

CREATE TABLE IF NOT EXISTS evidence_store_metadata (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS evidence_schema_migrations (
  migration_id TEXT PRIMARY KEY,
  schema_version TEXT NOT NULL,
  version INTEGER NOT NULL,
  description TEXT NOT NULL,
  applied_at TEXT NOT NULL
);
"""


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def read_json(path: Path) -> dict[str, Any]:
    data = json.loads(path.read_text(encoding="utf-8-sig"))
    if not isinstance(data, dict):
        raise ValueError(f"JSON root must be an object: {path}")
    return data


def as_dict(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def as_list(value: Any) -> list[Any]:
    if value is None:
        return []
    return value if isinstance(value, list) else [value]


def as_number(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except Exception:
        return None


def as_bool_int(value: Any) -> int | None:
    if value is None:
        return None
    return 1 if bool(value) else 0


def first_non_empty(*values: Any) -> Any:
    for value in values:
        if value not in (None, ""):
            return value
    return None


def scalar_or_none(value: Any) -> str | None:
    if value is None:
        return None
    if isinstance(value, (dict, list, tuple, set)):
        return None
    text = str(value).strip()
    return text if text else None


def first_scalar(*values: Any) -> str | None:
    for value in values:
        scalar = scalar_or_none(value)
        if scalar is not None:
            return scalar
    return None


def file_mtime_iso(path: Path) -> str | None:
    try:
        return datetime.fromtimestamp(path.stat().st_mtime, timezone.utc).isoformat()
    except OSError:
        return None


def file_size(path: Path) -> int | None:
    try:
        return int(path.stat().st_size)
    except OSError:
        return None


def sha256_text(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def strip_suffix_from_name(path: Path, prefix: str) -> str:
    name = path.name
    if name.startswith(prefix) and name.endswith(".json"):
        return name[len(prefix):-len(".json")]
    return path.stem


def derive_release_id(data: dict[str, Any], path: Path, spec: dict[str, Any]) -> str:
    filename_suffix = strip_suffix_from_name(path, str(spec["prefix"]))

    # Report files are named by release timestamp. Some objects, especially
    # supply-chain decisions, may carry image/app version as nested releaseId.
    # For EvidenceStore grouping, the timestamp suffix is the stable release key.
    compact_suffix = filename_suffix.replace("-", "")
    if (
        len(filename_suffix) == 15
        and filename_suffix[8] == "-"
        and compact_suffix.isdigit()
    ):
        return filename_suffix

    release = as_dict(data.get("release"))
    source = as_dict(data.get("source"))

    release_id = first_non_empty(
        data.get("releaseId"),
        release.get("releaseId"),
        source.get("releaseId"),
    )

    if release_id:
        return str(release_id)

    return filename_suffix


def derive_object_id(
    data: dict[str, Any],
    path: Path,
    spec: dict[str, Any],
    release_id: str,
) -> str:
    id_key = spec.get("id_key")
    if id_key and data.get(str(id_key)):
        return str(data[str(id_key)])

    nested_candidates = [
        as_dict(data.get("agent")).get("agentRunId"),
        as_dict(data.get("plan")).get("planRunId"),
        as_dict(data.get("executionRequest")).get("executionRequestId"),
        as_dict(data.get("supplyChain")).get("supplyChainDecisionId"),
    ]

    for candidate in nested_candidates:
        if candidate:
            return str(candidate)

    if spec["object_type"] == "releaseEvidence":
        return f"re-{release_id}"

    return f"{spec['id_prefix']}{strip_suffix_from_name(path, str(spec['prefix']))}"


def extract_release_fields(data: dict[str, Any], release_id: str) -> dict[str, Any]:
    release = as_dict(data.get("release"))
    resource = as_dict(data.get("resource"))
    summary = as_dict(data.get("summary"))
    observation = as_dict(data.get("observation"))
    policy = as_dict(data.get("policy"))
    recommendation = as_dict(data.get("recommendation"))
    risk = as_dict(data.get("risk"))
    image = as_dict(data.get("image"))

    return {
        "release_id": release_id,
        "service": first_scalar(data.get("service"), release.get("service"), resource.get("service")),
        "namespace": first_scalar(data.get("namespace"), release.get("namespace"), resource.get("namespace")),
        "env": first_scalar(data.get("env"), release.get("env"), resource.get("env")),
        "version": first_scalar(data.get("version"), release.get("version"), resource.get("version")),
        "commit_sha": first_scalar(data.get("commit"), release.get("commit"), resource.get("commit")),
        "image": first_scalar(image.get("image"), data.get("image")),
        "image_digest": first_scalar(
            data.get("imageDigest"),
            release.get("imageDigest"),
            image.get("imageDigest"),
            resource.get("imageDigest"),
        ),
        "release_result": first_scalar(
            data.get("releaseResult"),
            release.get("releaseResult"),
            observation.get("releaseResult"),
        ),
        "policy_decision": first_scalar(
            data.get("policyDecision"),
            release.get("policyDecision"),
            policy.get("policyDecision"),
        ),
        "final_action": first_scalar(
            data.get("finalAction"),
            release.get("finalAction"),
            release.get("recommendedAction"),
            recommendation.get("recommendedAction"),
        ),
        "risk_level": first_scalar(
            data.get("riskLevel"),
            summary.get("riskLevel"),
            observation.get("riskLevel"),
            risk.get("riskLevel"),
        ),
        "risk_score": as_number(first_non_empty(
            data.get("riskScore"),
            summary.get("riskScore"),
            observation.get("riskScore"),
            risk.get("riskScore"),
        )),
        "requires_human_approval": as_bool_int(first_non_empty(
            data.get("requiresHumanApproval"),
            release.get("requiresHumanApproval"),
            policy.get("requiresHumanApproval"),
        )),
        "generated_at": first_scalar(data.get("generatedAt"), release.get("generatedAt")),
    }




def normalized_verification_status(verification: dict[str, Any], results: dict[str, Any] | None = None) -> str | None:
    results = as_dict(results if results is not None else verification.get("results"))

    explicit = verification.get("verificationStatus") or results.get("verificationStatus")
    if explicit not in (None, ""):
        return str(explicit)

    mode = verification.get("mode")
    external_allowed = bool(results.get("externalVerificationAllowed"))
    external_executed = bool(results.get("externalVerificationExecuted"))
    external_succeeded = results.get("externalVerificationSucceeded")
    skipped_reason = results.get("externalVerificationSkippedReason")
    tool_available = bool(verification.get("toolAvailable"))

    if mode == "input_derived":
        return "input_derived"
    if mode == "admission":
        return "admission_placeholder"
    if mode != "external_command":
        return None

    if external_executed and external_succeeded is True:
        return "external_verification_passed"
    if external_executed and external_succeeded is False:
        return "external_verification_failed"
    if skipped_reason == "external_command_not_enabled" or not external_allowed:
        return "external_command_disabled"
    if skipped_reason == "tool_not_available" or not tool_available:
        return "external_tool_unavailable"
    return "external_verification_unavailable"

def compact_verification_summary(data: dict[str, Any]) -> dict[str, Any]:
    verification = as_dict(data.get("verification"))
    if not verification:
        return {}

    results = as_dict(verification.get("results"))
    guardrails = as_dict(verification.get("guardrails"))
    status = normalized_verification_status(verification, results)

    return {
        "schemaVersion": verification.get("schemaVersion"),
        "verificationStatus": status,
        "mode": verification.get("mode"),
        "tool": verification.get("tool"),
        "toolBinary": verification.get("toolBinary"),
        "toolAvailable": verification.get("toolAvailable"),
        "signatureVerified": results.get("signatureVerified"),
        "sbomPresent": results.get("sbomPresent"),
        "provenancePresent": results.get("provenancePresent"),
        "slsaLevelPresent": results.get("slsaLevelPresent"),
        "externalVerificationRequested": results.get("externalVerificationRequested"),
        "externalVerificationAllowed": results.get("externalVerificationAllowed"),
        "externalVerificationExecuted": results.get("externalVerificationExecuted"),
        "externalVerificationSucceeded": results.get("externalVerificationSucceeded"),
        "externalVerificationSkippedReason": results.get("externalVerificationSkippedReason"),
        "canRunExternalVerification": guardrails.get("canRunExternalVerification"),
        "doesNotRunExternalCommands": guardrails.get("doesNotRunExternalCommands"),
    }

def compact_object_summary(object_type: str, data: dict[str, Any]) -> dict[str, Any]:
    release = as_dict(data.get("release"))
    summary = as_dict(data.get("summary"))
    decision = as_dict(data.get("decision"))
    request = as_dict(data.get("request"))
    plan = as_dict(data.get("plan"))
    recommendation = as_dict(data.get("recommendation"))
    risk = as_dict(data.get("risk"))
    result_body = as_dict(data.get("result"))
    executor = as_dict(data.get("executor"))
    proposal = as_dict(data.get("proposal"))
    proposal_repo = as_dict(proposal.get("repository"))
    bundle = as_dict(data.get("bundle"))
    bundle_pr = as_dict(bundle.get("pullRequest"))
    handoff = as_dict(data.get("handoff"))
    request_body = as_dict(data.get("request"))
    delivery = as_dict(request_body.get("delivery"))
    verification_summary = compact_verification_summary(data)

    def pick(*values: Any) -> Any:
        for value in values:
            if value is not None and value != "":
                return value
        return None

    result = {
        "objectType": object_type,
        "schemaVersion": data.get("schemaVersion"),
        "generatedBy": data.get("generatedBy"),
        "generatedAt": data.get("generatedAt"),
        "releaseResult": first_non_empty(
            data.get("releaseResult"),
            release.get("releaseResult"),
            summary.get("releaseResult"),
        ),
        "policyDecision": first_non_empty(
            data.get("policyDecision"),
            release.get("policyDecision"),
        ),
        "finalAction": first_non_empty(
            data.get("finalAction"),
            release.get("recommendedAction"),
            recommendation.get("recommendedAction"),
        ),
        "riskLevel": first_non_empty(
            data.get("riskLevel"),
            summary.get("riskLevel"),
            risk.get("riskLevel"),
        ),
        "riskScore": first_non_empty(
            data.get("riskScore"),
            summary.get("riskScore"),
            risk.get("riskScore"),
        ),
        "requestedAction": request.get("requestedAction"),
        "requestStatus": request.get("requestStatus"),
        "decision": decision.get("decision"),
        "allowed": decision.get("allowed"),
        "willExecute": first_non_empty(
            as_dict(data.get("guardrails")).get("willExecute"),
            plan.get("willExecute"),
            request.get("willExecute"),
        ),
    }

    if object_type == "rolloutRuntimeInspect":
        target = as_dict(data.get("target"))
        rollout = as_dict(data.get("rollout"))
        analysis = as_dict(data.get("analysis"))
        guardrails = as_dict(data.get("guardrails"))

        result["rolloutName"] = first_non_empty(target.get("rolloutName"), rollout.get("name"))
        result["namespace"] = first_non_empty(target.get("namespace"), rollout.get("namespace"))
        result["service"] = first_non_empty(target.get("service"), release.get("service"))
        result["env"] = first_non_empty(target.get("env"), release.get("env"))
        result["rolloutPhase"] = rollout.get("phase")
        result["strategy"] = rollout.get("strategy")
        result["currentStepIndex"] = rollout.get("currentStepIndex")
        result["replicas"] = rollout.get("replicas")
        result["updatedReplicas"] = rollout.get("updatedReplicas")
        result["readyReplicas"] = rollout.get("readyReplicas")
        result["availableReplicas"] = rollout.get("availableReplicas")
        result["desiredWeight"] = rollout.get("desiredWeight")
        result["actualWeight"] = rollout.get("actualWeight")
        result["paused"] = rollout.get("paused")
        result["degraded"] = rollout.get("degraded")
        result["analysisStatus"] = analysis.get("status")
        result["analysisRunName"] = analysis.get("analysisRunName")
        result["readOnly"] = guardrails.get("readOnly")
        result["dryRunOnly"] = guardrails.get("dryRunOnly")
        result["willExecute"] = guardrails.get("willExecute")
        result["doesNotModifyKubernetes"] = guardrails.get("doesNotModifyKubernetes")

    if object_type == "runtimeActionRecommendation":
        target = as_dict(data.get("target"))
        runtime_snapshot = as_dict(data.get("runtimeSnapshot"))
        evidence_refs = as_dict(data.get("evidenceRefs"))
        guardrails = as_dict(data.get("guardrails"))

        result["runtimeActionRecommendationId"] = data.get("runtimeActionRecommendationId")
        result["recommendationStatus"] = recommendation.get("recommendationStatus")
        result["recommendedAction"] = recommendation.get("recommendedAction")
        result["riskLevel"] = recommendation.get("riskLevel")
        result["confidence"] = recommendation.get("confidence")
        result["approvalRequired"] = recommendation.get("approvalRequired")
        result["reasons"] = recommendation.get("reasons") or []
        result["rolloutName"] = target.get("rolloutName")
        result["namespace"] = target.get("namespace")
        result["service"] = first_non_empty(target.get("service"), release.get("service"))
        result["env"] = first_non_empty(target.get("env"), release.get("env"))
        result["rolloutPhase"] = runtime_snapshot.get("rolloutPhase")
        result["analysisStatus"] = runtime_snapshot.get("analysisStatus")
        result["sourceRolloutRuntimeInspectId"] = evidence_refs.get("sourceRolloutRuntimeInspectId")
        result["sourceRolloutRuntimeInspect"] = evidence_refs.get("rolloutRuntimeInspect")
        result["readOnly"] = guardrails.get("readOnly")
        result["recommendationOnly"] = guardrails.get("recommendationOnly")
        result["willExecute"] = guardrails.get("willExecute")
        result["doesNotModifyKubernetes"] = guardrails.get("doesNotModifyKubernetes")

    if object_type == "runtimeActionRequest":
        request_body = as_dict(data.get("request"))
        recommendation_binding = as_dict(data.get("recommendationBinding"))
        runtime_snapshot = as_dict(data.get("runtimeSnapshot"))
        approval = as_dict(data.get("approval"))
        target = as_dict(data.get("target"))
        evidence_refs = as_dict(data.get("evidenceRefs"))
        guardrails = as_dict(data.get("guardrails"))

        result["runtimeActionRequestId"] = data.get("runtimeActionRequestId")
        result["sourceRuntimeActionRecommendationId"] = data.get("sourceRuntimeActionRecommendationId")
        result["requestedAction"] = request_body.get("requestedAction")
        result["requestStatus"] = request_body.get("requestStatus")
        result["lifecycleStage"] = request_body.get("lifecycleStage")
        result["riskLevel"] = request_body.get("riskLevel")
        result["confidence"] = request_body.get("confidence")
        result["approvalRequired"] = request_body.get("approvalRequired")
        result["readyToExecute"] = request_body.get("readyToExecute")
        result["recommendationStatus"] = recommendation_binding.get("recommendationStatus")
        result["recommendedAction"] = recommendation_binding.get("recommendedAction")
        result["allowedToRequest"] = recommendation_binding.get("allowedToRequest")
        result["blockingReasons"] = recommendation_binding.get("blockingReasons") or []
        result["approvalStatus"] = approval.get("status")
        result["approved"] = approval.get("approved")
        result["rolloutName"] = target.get("rolloutName")
        result["namespace"] = target.get("namespace")
        result["service"] = first_non_empty(target.get("service"), release.get("service"))
        result["env"] = first_non_empty(target.get("env"), release.get("env"))
        result["rolloutPhase"] = runtime_snapshot.get("rolloutPhase")
        result["analysisStatus"] = runtime_snapshot.get("analysisStatus")
        result["sourceRuntimeActionRecommendation"] = evidence_refs.get("runtimeActionRecommendation")
        result["sourceRolloutRuntimeInspect"] = evidence_refs.get("rolloutRuntimeInspect")
        result["sourceRolloutRuntimeInspectId"] = evidence_refs.get("sourceRolloutRuntimeInspectId")
        result["requestOnly"] = guardrails.get("requestOnly")
        result["readOnly"] = guardrails.get("readOnly")
        result["willExecute"] = guardrails.get("willExecute")
        result["doesNotPause"] = guardrails.get("doesNotPause")
        result["doesNotModifyKubernetes"] = guardrails.get("doesNotModifyKubernetes")

    if object_type == "runtimeActionPreflight":
        request_body = as_dict(data.get("request"))
        preflight = as_dict(data.get("preflight"))
        runtime_snapshot = as_dict(data.get("runtimeSnapshot"))
        target = as_dict(data.get("target"))
        evidence_refs = as_dict(data.get("evidenceRefs"))
        guardrails = as_dict(data.get("guardrails"))

        result["runtimeActionPreflightId"] = data.get("runtimeActionPreflightId")
        result["sourceRuntimeActionRequestId"] = data.get("sourceRuntimeActionRequestId")
        result["requestedAction"] = request_body.get("requestedAction")
        result["requestStatus"] = request_body.get("requestStatus")
        result["lifecycleStage"] = request_body.get("lifecycleStage")
        result["riskLevel"] = request_body.get("riskLevel")
        result["confidence"] = request_body.get("confidence")
        result["approvalRequired"] = request_body.get("approvalRequired")
        result["approved"] = request_body.get("approved")
        result["allowedToRequest"] = request_body.get("allowedToRequest")
        result["preflightStatus"] = preflight.get("preflightStatus")
        result["eligibilityStatus"] = preflight.get("eligibilityStatus")
        result["blockingReasons"] = preflight.get("blockingReasons") or []
        result["approvalReasons"] = preflight.get("approvalReasons") or []
        result["warningReasons"] = preflight.get("warningReasons") or []
        result["eligibleForExecution"] = preflight.get("eligibleForExecution")
        result["readyToExecute"] = preflight.get("readyToExecute")
        result["rolloutName"] = target.get("rolloutName")
        result["namespace"] = target.get("namespace")
        result["service"] = first_non_empty(target.get("service"), release.get("service"))
        result["env"] = first_non_empty(target.get("env"), release.get("env"))
        result["rolloutPhase"] = runtime_snapshot.get("rolloutPhase")
        result["analysisStatus"] = runtime_snapshot.get("analysisStatus")
        result["sourceRuntimeActionRequest"] = evidence_refs.get("runtimeActionRequest")
        result["sourceRuntimeActionRecommendation"] = evidence_refs.get("runtimeActionRecommendation")
        result["sourceRuntimeActionRecommendationId"] = evidence_refs.get("sourceRuntimeActionRecommendationId")
        result["sourceRolloutRuntimeInspect"] = evidence_refs.get("rolloutRuntimeInspect")
        result["sourceRolloutRuntimeInspectId"] = evidence_refs.get("sourceRolloutRuntimeInspectId")
        result["preflightOnly"] = guardrails.get("preflightOnly")
        result["readOnly"] = guardrails.get("readOnly")
        result["willExecute"] = guardrails.get("willExecute")
        result["doesNotPause"] = guardrails.get("doesNotPause")
        result["doesNotModifyKubernetes"] = guardrails.get("doesNotModifyKubernetes")

    if object_type == "runtimeActionExecutionResult":
        action = as_dict(data.get("action"))
        result_body = as_dict(data.get("result"))
        target = as_dict(data.get("target"))
        executor = as_dict(data.get("executor"))
        write_gate = as_dict(data.get("writeGate"))
        before_snapshot = as_dict(data.get("beforeSnapshot"))
        after_snapshot = as_dict(data.get("afterSnapshot"))
        receipt = as_dict(data.get("receipt"))
        evidence_refs = as_dict(data.get("evidenceRefs"))
        guardrails = as_dict(data.get("guardrails"))
        post_action_verification = as_dict(data.get("postActionVerification"))
        actor_boundary = as_dict(data.get("actorBoundary"))
        recovery_boundary = as_dict(data.get("recoveryBoundary"))
        execution_safety_boundary = as_dict(data.get("executionSafetyBoundary"))
        actor_executor_identity = as_dict(actor_boundary.get("executorIdentity"))
        actor_rbac_boundary = as_dict(actor_boundary.get("rbacBoundary"))
        recovery_retry = as_dict(recovery_boundary.get("retry"))
        recovery_failure = as_dict(recovery_boundary.get("failureRecovery"))
        safety_default_policy = as_dict(execution_safety_boundary.get("defaultPolicy"))
        safety_operation_risk = as_dict(execution_safety_boundary.get("operationRisk"))
        safety_decision = as_dict(execution_safety_boundary.get("safetyDecision"))

        verification_status = (
            post_action_verification.get("verificationStatus")
            or result_body.get("verificationStatus")
            or receipt.get("verificationStatus")
        )
        pause_verified = post_action_verification.get("pauseVerified")
        if pause_verified is None:
            pause_verified = result_body.get("pauseVerified")
        if pause_verified is None:
            pause_verified = receipt.get("pauseVerified")
        resume_verified = post_action_verification.get("resumeVerified")
        if resume_verified is None:
            resume_verified = result_body.get("resumeVerified")
        if resume_verified is None:
            resume_verified = receipt.get("resumeVerified")
        promote_verified = post_action_verification.get("promoteVerified")
        if promote_verified is None:
            promote_verified = result_body.get("promoteVerified")
        if promote_verified is None:
            promote_verified = receipt.get("promoteVerified")
        abort_verified = post_action_verification.get("abortVerified")
        if abort_verified is None:
            abort_verified = result_body.get("abortVerified")
        if abort_verified is None:
            abort_verified = receipt.get("abortVerified")
        rollback_verified = post_action_verification.get("rollbackVerified")
        if rollback_verified is None:
            rollback_verified = result_body.get("rollbackVerified")
        if rollback_verified is None:
            rollback_verified = receipt.get("rollbackVerified")
        post_action_observed = post_action_verification.get("postActionObserved")
        if post_action_observed is None:
            post_action_observed = result_body.get("postActionObserved")
        desired_state_observed = post_action_verification.get("desiredStateObserved")
        if desired_state_observed is None:
            desired_state_observed = result_body.get("desiredStateObserved")

        result["runtimeActionExecutionResultId"] = data.get("runtimeActionExecutionResultId")
        result["sourceRuntimeActionPreflightId"] = data.get("sourceRuntimeActionPreflightId")
        result["sourceRuntimeActionRequestId"] = data.get("sourceRuntimeActionRequestId")
        result["requestedAction"] = first_non_empty(action.get("requestedAction"), result_body.get("requestedAction"))
        result["actionStatus"] = first_non_empty(action.get("actionStatus"), result_body.get("actionStatus"))
        result["executionStatus"] = result_body.get("executionStatus")
        result["commandMode"] = action.get("commandMode")
        result["rollbackTarget"] = as_dict(data.get("rollbackTarget"))
        result["commandExitCode"] = action.get("commandExitCode")
        result["commandWillExecute"] = action.get("commandWillExecute")
        result["didPause"] = result_body.get("didPause")
        result["didResume"] = result_body.get("didResume")
        result["didPromote"] = result_body.get("didPromote")
        result["didAbort"] = result_body.get("didAbort")
        result["didRollback"] = result_body.get("didRollback")

        execution_summary = as_dict(data.get("executionSummary"))
        if execution_summary:
            result["executionSummary"] = execution_summary
            result["didExecute"] = execution_summary.get("didExecute")
            result["executionVerified"] = execution_summary.get("verified")

        gate_summary = as_dict(data.get("gateSummary"))
        if gate_summary:
            result["gateSummary"] = gate_summary
            result["gateOverall"] = gate_summary.get("overall")
            result["gateFinalExecute"] = gate_summary.get("finalExecute")

        verification_summary = as_dict(data.get("verificationSummary"))
        if verification_summary:
            result["verificationSummary"] = verification_summary
            result["verificationSummaryStatus"] = verification_summary.get("status")
            result["actionVerified"] = verification_summary.get("actionVerified")

        risk_summary = as_dict(data.get("riskSummary"))
        if risk_summary:
            result["riskSummary"] = risk_summary
            result["runtimeRiskLevel"] = risk_summary.get("riskLevel")
            result["runtimeDefaultOff"] = risk_summary.get("defaultOff")
        result["attemptedKubernetesMutation"] = result_body.get("attemptedKubernetesMutation")
        result["mutatedKubernetes"] = result_body.get("mutatedKubernetes")
        result["mutatedGitOps"] = result_body.get("mutatedGitOps")
        result["didModifyKubernetes"] = receipt.get("didModifyKubernetes")
        result["didModifyGitOps"] = receipt.get("didModifyGitOps")
        result["executorName"] = executor.get("executorName")
        result["executorAdapter"] = executor.get("adapter")
        result["rolloutName"] = target.get("rolloutName")
        result["namespace"] = target.get("namespace")
        result["service"] = first_non_empty(target.get("service"), release.get("service"))
        result["env"] = first_non_empty(target.get("env"), release.get("env"))
        result["rolloutPhase"] = before_snapshot.get("rolloutPhase")
        result["analysisStatus"] = before_snapshot.get("analysisStatus")
        result["afterObservationMode"] = after_snapshot.get("observationMode")
        result["verificationStatus"] = verification_status
        result["pauseVerified"] = pause_verified
        result["resumeVerified"] = resume_verified
        result["promoteVerified"] = promote_verified
        result["abortVerified"] = abort_verified
        result["rollbackVerified"] = rollback_verified
        result["postActionObserved"] = post_action_observed
        result["desiredStateObserved"] = desired_state_observed
        result["preflightStatus"] = write_gate.get("preflightStatus")
        result["eligibilityStatus"] = write_gate.get("eligibilityStatus")
        result["finalExecuteEnabled"] = write_gate.get("finalExecuteEnabled")
        result["writeAllowed"] = write_gate.get("writeAllowed")
        result["sourceRuntimeActionPreflight"] = evidence_refs.get("runtimeActionPreflight")
        result["sourceRuntimeActionRequest"] = evidence_refs.get("runtimeActionRequest")
        result["sourceRuntimeActionRecommendation"] = evidence_refs.get("runtimeActionRecommendation")
        result["sourceRolloutRuntimeInspect"] = evidence_refs.get("rolloutRuntimeInspect")
        result["sourceRolloutRuntimeInspectId"] = evidence_refs.get("sourceRolloutRuntimeInspectId")
        result["readOnly"] = guardrails.get("readOnly")
        result["dryRunOnly"] = guardrails.get("dryRunOnly")
        result["willExecute"] = guardrails.get("willExecute")
        result["doesNotModifyGitOps"] = guardrails.get("doesNotModifyGitOps")
        result["actorBoundary"] = actor_boundary
        result["recoveryBoundary"] = recovery_boundary
        result["executionSafetyBoundary"] = execution_safety_boundary
        result["actorRuntimeIdentity"] = actor_executor_identity.get("runtimeIdentity")
        result["actorKubernetesIdentity"] = actor_executor_identity.get("kubernetesIdentity")
        result["watcherRbacMode"] = actor_rbac_boundary.get("watcherRbacMode")
        result["watcherCanMutateKubernetes"] = actor_rbac_boundary.get("watcherCanMutateKubernetes")
        result["manualRetryAllowed"] = recovery_retry.get("manualRetryAllowed")
        result["recoveryFailureMode"] = recovery_failure.get("failureMode")
        result["executionDefaultOff"] = safety_default_policy.get("defaultOff")
        result["executionSafetyRiskLevel"] = safety_operation_risk.get("riskLevel")
        result["executionDirectExecutionAllowed"] = safety_decision.get("directExecutionAllowed")
        result["executionSafetyBlockingReasons"] = safety_decision.get("blockingReasons") or []

    if object_type == "policyRuntimeResult":
        policy_decision = as_dict(data.get("policyDecision"))
        runtime_summary = as_dict(data.get("summary"))
        safety = as_dict(data.get("safety"))

        result["policyDecision"] = pick(
            policy_decision.get("policyDecision"),
            runtime_summary.get("policyDecision"),
        )
        result["finalAction"] = pick(
            policy_decision.get("finalAction"),
            runtime_summary.get("finalAction"),
        )
        result["allowed"] = pick(
            policy_decision.get("allowed"),
            runtime_summary.get("allowed"),
        )
        result["requiresHumanApproval"] = pick(
            policy_decision.get("requiresHumanApproval"),
            runtime_summary.get("requiresHumanApproval"),
        )
        result["requestedAction"] = pick(
            policy_decision.get("requestedAction"),
            runtime_summary.get("requestedAction"),
        )
        result["matchedRules"] = pick(
            policy_decision.get("matchedRules"),
            runtime_summary.get("matchedRules"),
            [],
        )
        result["deniedReasons"] = pick(policy_decision.get("deniedReasons"), [])
        result["approvalRequiredReasons"] = pick(policy_decision.get("approvalRequiredReasons"), [])
        result["runtimeStatus"] = runtime_summary.get("runtimeStatus")
        result["runtimePreviewOnly"] = runtime_summary.get("runtimePreviewOnly")
        result["runtimeExternalCommandExecuted"] = runtime_summary.get("runtimeExternalCommandExecuted")
        result["willExecute"] = pick(safety.get("willExecute"), result.get("willExecute"))

    if object_type == "policyDecision":
        safety = as_dict(data.get("safety"))

        result["policyDecision"] = data.get("policyDecision")
        result["finalAction"] = data.get("finalAction")
        result["allowed"] = data.get("allowed")
        result["requiresHumanApproval"] = data.get("requiresHumanApproval")
        result["requestedAction"] = data.get("requestedAction")
        result["matchedRules"] = data.get("matchedRules") or []
        result["deniedReasons"] = data.get("deniedReasons") or []
        result["approvalRequiredReasons"] = data.get("approvalRequiredReasons") or []
        result["willExecute"] = pick(safety.get("willExecute"), result.get("willExecute"))

    if verification_summary:
        result["verification"] = verification_summary
        result["verificationMode"] = verification_summary.get("mode")
        result["verificationToolAvailable"] = verification_summary.get("toolAvailable")
        result["signatureVerified"] = verification_summary.get("signatureVerified")
        result["sbomPresent"] = verification_summary.get("sbomPresent")
        result["provenancePresent"] = verification_summary.get("provenancePresent")
        result["canRunExternalVerification"] = verification_summary.get("canRunExternalVerification")

    observability = as_dict(data.get("observability"))
    source = as_dict(data.get("source"))
    guardrails = as_dict(data.get("guardrails"))
    write_gate = as_dict(data.get("writeGate"))

    for guardrail_key in [
        "readOnly",
        "dryRunOnly",
        "willExecute",
        "doesNotCommit",
        "doesNotPush",
        "doesNotCreatePullRequest",
        "didMaterializeFiles",
        "didCreateLocalCommit",
        "didPushBranch",
        "didCreatePullRequest",
        "didClosePullRequest",
        "didDeleteRemoteBranch",
        "doesNotMergePullRequest",
        "doesNotModifyKubernetes",
    ]:
        guardrail_value = guardrails.get(guardrail_key)
        if guardrail_value is not None:
            result[guardrail_key] = guardrail_value

    for source_key, result_key in [
        ("enabled", "writeGateEnabled"),
        ("allowEnv", "writeGateAllowEnv"),
        ("allowValue", "writeGateAllowValue"),
        ("operationEnv", "writeGateOperationEnv"),
        ("requiredOperation", "writeGateRequiredOperation"),
        ("operation", "writeGateOperation"),
    ]:
        write_gate_value = write_gate.get(source_key)
        if write_gate_value is not None and write_gate_value != "":
            result[result_key] = write_gate_value

    trace_id = first_non_empty(
        data.get("traceId"),
        observability.get("traceId"),
    )
    agent_trace_id = first_non_empty(
        data.get("agentTraceId"),
        observability.get("agentTraceId"),
        source.get("agentTraceId"),
        summary.get("sourceAgentTraceId"),
    )
    root_span_id = first_non_empty(
        data.get("rootSpanId"),
        observability.get("rootSpanId"),
    )

    if trace_id:
        result["traceId"] = trace_id
    if agent_trace_id:
        result["agentTraceId"] = agent_trace_id
    if root_span_id:
        result["rootSpanId"] = root_span_id


    if object_type == "otelSpanBundle":
        result["spanCount"] = summary.get("spanCount")
        result["hasRootSpan"] = summary.get("hasRootSpan")
        result["sourceAgentTraceId"] = summary.get("sourceAgentTraceId")
        result["spanNames"] = summary.get("spanNames") or []
        result["localFileOnly"] = guardrails.get("localFileOnly")
        result["doesNotSendExternalTelemetry"] = guardrails.get("doesNotSendExternalTelemetry")
        result["doesNotCallExternalCollector"] = guardrails.get("doesNotCallExternalCollector")

    if object_type in ("releaseEvidence", "evidenceRecord"):
        artifacts = as_dict(data.get("artifacts"))
        result["agentTrace"] = first_non_empty(
            observability.get("agentTrace"),
            artifacts.get("agentTrace"),
        )
        result["otelSpanBundle"] = first_non_empty(
            observability.get("otelSpanBundle"),
            artifacts.get("otelSpanBundle"),
        )
        result["localFileOnly"] = observability.get("localFileOnly")
        result["doesNotSendExternalTelemetry"] = observability.get("doesNotSendExternalTelemetry")
        result["doesNotCallExternalCollector"] = observability.get("doesNotCallExternalCollector")

    return result



def sqlite_user_version(conn: sqlite3.Connection) -> int:
    row = conn.execute("PRAGMA user_version").fetchone()
    if row is None:
        return 0
    return int(row[0] or 0)


def set_sqlite_user_version(conn: sqlite3.Connection, version: int) -> None:
    safe_version = int(version)
    conn.execute(f"PRAGMA user_version = {safe_version}")


def record_schema_metadata(conn: sqlite3.Connection) -> None:
    applied_at = now_iso()

    for migration in SCHEMA_MIGRATIONS:
        conn.execute(
            """
            INSERT OR IGNORE INTO evidence_schema_migrations (
              migration_id, schema_version, version, description, applied_at
            ) VALUES (?, ?, ?, ?, ?)
            """,
            (
                migration["migrationId"],
                migration["schemaVersion"],
                migration["version"],
                migration["description"],
                applied_at,
            ),
        )

    metadata = {
        "schemaVersion": CURRENT_DB_SCHEMA_ID,
        "currentVersion": str(CURRENT_DB_SCHEMA_VERSION),
        "migrationCount": str(len(SCHEMA_MIGRATIONS)),
    }

    for key, value in metadata.items():
        conn.execute(
            """
            INSERT OR REPLACE INTO evidence_store_metadata (
              key, value, updated_at
            ) VALUES (?, ?, ?)
            """,
            (key, value, applied_at),
        )

    set_sqlite_user_version(conn, CURRENT_DB_SCHEMA_VERSION)


def schema_state(conn: sqlite3.Connection) -> dict[str, Any]:
    migrations = [
        dict(row)
        for row in conn.execute(
            """
            SELECT migration_id, schema_version, version, description, applied_at
            FROM evidence_schema_migrations
            ORDER BY version ASC, migration_id ASC
            """
        ).fetchall()
    ]

    metadata = {
        str(row["key"]): str(row["value"])
        for row in conn.execute(
            """
            SELECT key, value
            FROM evidence_store_metadata
            ORDER BY key ASC
            """
        ).fetchall()
    }

    return {
        "schemaVersion": "evidence.store.schema/v1alpha1",
        "generatedAt": now_iso(),
        "storeSchemaVersion": CURRENT_DB_SCHEMA_ID,
        "currentVersion": CURRENT_DB_SCHEMA_VERSION,
        "sqliteUserVersion": sqlite_user_version(conn),
        "metadata": metadata,
        "migrationCount": len(migrations),
        "migrations": migrations,
    }

def init_db(conn: sqlite3.Connection) -> None:
    conn.executescript(SCHEMA_SQL)
    record_schema_metadata(conn)
    conn.commit()

def upsert_release(conn: sqlite3.Connection, fields: dict[str, Any], seen_at: str) -> None:
    conn.execute(
        """
        INSERT INTO releases (
          release_id, service, namespace, env, version, commit_sha, image, image_digest,
          release_result, policy_decision, final_action, risk_level, risk_score,
          requires_human_approval, generated_at, first_seen_at, last_seen_at
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(release_id) DO UPDATE SET
          service = COALESCE(excluded.service, releases.service),
          namespace = COALESCE(excluded.namespace, releases.namespace),
          env = COALESCE(excluded.env, releases.env),
          version = COALESCE(excluded.version, releases.version),
          commit_sha = COALESCE(excluded.commit_sha, releases.commit_sha),
          image = COALESCE(excluded.image, releases.image),
          image_digest = COALESCE(excluded.image_digest, releases.image_digest),
          release_result = COALESCE(excluded.release_result, releases.release_result),
          policy_decision = COALESCE(excluded.policy_decision, releases.policy_decision),
          final_action = COALESCE(excluded.final_action, releases.final_action),
          risk_level = COALESCE(excluded.risk_level, releases.risk_level),
          risk_score = COALESCE(excluded.risk_score, releases.risk_score),
          requires_human_approval = COALESCE(excluded.requires_human_approval, releases.requires_human_approval),
          generated_at = COALESCE(excluded.generated_at, releases.generated_at),
          last_seen_at = excluded.last_seen_at
        """,
        (
            fields["release_id"],
            fields.get("service"),
            fields.get("namespace"),
            fields.get("env"),
            fields.get("version"),
            fields.get("commit_sha"),
            fields.get("image"),
            fields.get("image_digest"),
            fields.get("release_result"),
            fields.get("policy_decision"),
            fields.get("final_action"),
            fields.get("risk_level"),
            fields.get("risk_score"),
            fields.get("requires_human_approval"),
            fields.get("generated_at"),
            seen_at,
            seen_at,
        ),
    )


def insert_artifacts(
    conn: sqlite3.Connection,
    release_id: str,
    object_pk: str,
    data: dict[str, Any],
) -> int:
    count = 0

    containers = []
    if isinstance(data.get("artifacts"), dict):
        containers.append(("artifacts", as_dict(data.get("artifacts"))))
    if isinstance(data.get("links"), dict):
        containers.append(("links", as_dict(data.get("links"))))

    for _, container in containers:
        for artifact_kind, value in container.items():
            if value in (None, ""):
                continue

            exists_flag = None
            content_type = None
            size_bytes = None
            modified_at = None

            if isinstance(value, dict):
                path = value.get("path") or value.get("file")
                exists_flag = as_bool_int(value.get("exists"))
                content_type = value.get("contentType")
                size_bytes = value.get("sizeBytes")
                modified_at = value.get("modifiedAt")
            else:
                path = str(value)

            if not path:
                continue

            conn.execute(
                """
                INSERT OR REPLACE INTO release_artifacts (
                  release_id, artifact_kind, path, exists_flag,
                  content_type, size_bytes, modified_at, source_object_pk
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    release_id,
                    str(artifact_kind),
                    str(path),
                    exists_flag,
                    content_type,
                    size_bytes,
                    modified_at,
                    object_pk,
                ),
            )
            count += 1

    return count


def import_file(conn: sqlite3.Connection, path: Path, spec: dict[str, Any]) -> tuple[str, str]:
    raw_text = path.read_text(encoding="utf-8-sig")
    data = json.loads(raw_text)
    if not isinstance(data, dict):
        raise ValueError(f"JSON root must be object: {path}")

    imported_at = now_iso()
    object_type = str(spec["object_type"])
    release_id = derive_release_id(data, path, spec)
    object_id = derive_object_id(data, path, spec, release_id)
    object_pk = f"{object_type}:{release_id}:{object_id}"

    release_fields = extract_release_fields(data, release_id)
    upsert_release(conn, release_fields, imported_at)

    summary = compact_object_summary(object_type, data)

    conn.execute(
        """
        INSERT INTO evidence_objects (
          object_pk, object_type, object_id, release_id, schema_version,
          source_path, source_mtime, content_sha256, generated_at,
          imported_at, summary_json, raw_json
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(object_pk) DO UPDATE SET
          schema_version = excluded.schema_version,
          source_path = excluded.source_path,
          source_mtime = excluded.source_mtime,
          content_sha256 = excluded.content_sha256,
          generated_at = excluded.generated_at,
          imported_at = excluded.imported_at,
          summary_json = excluded.summary_json,
          raw_json = excluded.raw_json
        """,
        (
            object_pk,
            object_type,
            object_id,
            release_id,
            data.get("schemaVersion"),
            str(path),
            file_mtime_iso(path),
            sha256_text(raw_text),
            data.get("generatedAt"),
            imported_at,
            json.dumps(summary, ensure_ascii=False, sort_keys=True),
            json.dumps(data, ensure_ascii=False, sort_keys=True),
        ),
    )

    insert_artifacts(conn, release_id, object_pk, data)

    return release_id, object_type


def import_dir(conn: sqlite3.Connection, report_dir: Path) -> dict[str, Any]:
    if not report_dir.is_dir():
        raise SystemExit(f"ERROR: report dir does not exist: {report_dir}")

    init_db(conn)

    imported = 0
    skipped = 0
    by_type: dict[str, int] = {}
    release_ids: set[str] = set()

    for spec in RESOURCE_SPECS:
        latest_name = str(spec["latest"])
        for path in sorted(report_dir.glob(str(spec["glob"]))):
            if path.name == latest_name:
                skipped += 1
                continue
            try:
                release_id, object_type = import_file(conn, path, spec)
            except Exception as exc:
                print(f"WARN: failed to import {path}: {exc}", file=sys.stderr)
                skipped += 1
                continue

            imported += 1
            release_ids.add(release_id)
            by_type[object_type] = by_type.get(object_type, 0) + 1

    conn.commit()

    return {
        "schemaVersion": "evidence.store.import/v1alpha1",
        "reportDir": str(report_dir),
        "importedObjects": imported,
        "skippedObjects": skipped,
        "releaseCount": len(release_ids),
        "byType": dict(sorted(by_type.items())),
    }


def row_to_dict(row: sqlite3.Row | None) -> dict[str, Any] | None:
    if row is None:
        return None
    return {key: row[key] for key in row.keys()}


def parse_json_field(value: Any) -> dict[str, Any]:
    if value in (None, ""):
        return {}
    if isinstance(value, dict):
        return value
    try:
        data = json.loads(str(value))
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


def normalize_release_row(row: sqlite3.Row | None) -> dict[str, Any] | None:
    item = row_to_dict(row)
    if item is None:
        return None

    if item.get("requires_human_approval") is not None:
        item["requires_human_approval"] = bool(item["requires_human_approval"])

    return item


def normalize_object_row(row: sqlite3.Row | None, include_raw: bool) -> dict[str, Any] | None:
    item = row_to_dict(row)
    if item is None:
        return None

    item["summary"] = parse_json_field(item.pop("summary_json", None))
    raw_json = item.pop("raw_json", None)

    if include_raw:
        item["raw"] = parse_json_field(raw_json)

    return item


def list_releases(
    conn: sqlite3.Connection,
    limit: int,
    service: str | None = None,
    env: str | None = None,
    release_result: str | None = None,
) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    where = []
    params: list[Any] = []

    if service:
        where.append("r.service = ?")
        params.append(service)

    if env:
        where.append("r.env = ?")
        params.append(env)

    if release_result:
        where.append("r.release_result = ?")
        params.append(release_result)

    where_sql = ""
    if where:
        where_sql = "WHERE " + " AND ".join(where)

    safe_limit = max(1, min(int(limit), 500))

    rows = conn.execute(
        f"""
        SELECT
          r.*,
          COUNT(e.object_pk) AS object_count,
          MAX(e.imported_at) AS latest_object_imported_at
        FROM releases r
        LEFT JOIN evidence_objects e ON e.release_id = r.release_id
        {where_sql}
        GROUP BY r.release_id
        ORDER BY
          r.release_id DESC,
          COALESCE(r.generated_at, r.last_seen_at) DESC
        LIMIT ?
        """,
        (*params, safe_limit),
    ).fetchall()

    items = []
    for row in rows:
        item = normalize_release_row(row) or {}

        object_rows = conn.execute(
            """
            SELECT object_type, object_id
            FROM evidence_objects
            WHERE release_id = ?
            ORDER BY object_type, object_id
            """,
            (item.get("release_id"),),
        ).fetchall()

        item["object_types"] = sorted({object_row["object_type"] for object_row in object_rows})
        item["objects"] = [
            {
                "objectType": object_row["object_type"],
                "objectId": object_row["object_id"],
            }
            for object_row in object_rows
        ]
        items.append(item)

    return {
        "schemaVersion": "evidence.store.releaseList/v1alpha1",
        "generatedAt": now_iso(),
        "count": len(items),
        "limit": safe_limit,
        "filters": {
            "service": service,
            "env": env,
            "releaseResult": release_result,
        },
        "items": items,
    }


def get_object(
    conn: sqlite3.Connection,
    object_type: str,
    object_id: str,
    release_id: str | None,
    include_raw: bool,
) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    where = [
        "object_type = ?",
        "object_id = ?",
    ]
    params: list[Any] = [object_type, object_id]

    if release_id:
        where.append("release_id = ?")
        params.append(release_id)

    row = conn.execute(
        f"""
        SELECT object_type, object_id, release_id, schema_version,
               source_path, source_mtime, content_sha256, generated_at,
               imported_at, summary_json, raw_json
        FROM evidence_objects
        WHERE {" AND ".join(where)}
        ORDER BY imported_at DESC, source_mtime DESC
        LIMIT 1
        """,
        tuple(params),
    ).fetchone()

    obj = normalize_object_row(row, include_raw)
    if obj is None:
        raise SystemExit(
            "ERROR: object not found: "
            f"objectType={object_type} objectId={object_id}"
            + (f" releaseId={release_id}" if release_id else "")
        )

    release = normalize_release_row(
        conn.execute(
            "SELECT * FROM releases WHERE release_id = ?",
            (obj["release_id"],),
        ).fetchone()
    )

    return {
        "schemaVersion": "evidence.store.object/v1alpha1",
        "generatedAt": now_iso(),
        "release": normalize_release_row(release),
        "object": obj,
    }



def list_artifacts(
    conn: sqlite3.Connection,
    limit: int,
    release_id: str | None = None,
    artifact_kind: str | None = None,
) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    where = []
    params: list[Any] = []

    if release_id:
        where.append("release_id = ?")
        params.append(release_id)

    if artifact_kind:
        where.append("artifact_kind = ?")
        params.append(artifact_kind)

    where_sql = ""
    if where:
        where_sql = "WHERE " + " AND ".join(where)

    safe_limit = max(1, min(int(limit), 500))

    rows = conn.execute(
        f"""
        SELECT release_id, artifact_kind, path, exists_flag, content_type,
               size_bytes, modified_at, source_object_pk
        FROM release_artifacts
        {where_sql}
        ORDER BY release_id DESC, artifact_kind ASC, path ASC
        LIMIT ?
        """,
        (*params, safe_limit),
    ).fetchall()

    items = []
    for row in rows:
        item = row_to_dict(row) or {}
        if item.get("exists_flag") is not None:
            item["exists"] = bool(item.pop("exists_flag"))
        else:
            item.pop("exists_flag", None)
        items.append(item)

    return {
        "schemaVersion": "evidence.store.artifactList/v1alpha1",
        "generatedAt": now_iso(),
        "count": len(items),
        "limit": safe_limit,
        "filters": {
            "releaseId": release_id,
            "artifactKind": artifact_kind,
        },
        "items": items,
    }


def search_objects(
    conn: sqlite3.Connection,
    query: str | None,
    limit: int,
    object_type: str | None = None,
    release_id: str | None = None,
    include_raw: bool = False,
) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    where = []
    params: list[Any] = []

    if object_type:
        where.append("object_type = ?")
        params.append(object_type)

    if release_id:
        where.append("release_id = ?")
        params.append(release_id)

    normalized_query = (query or "").strip()
    if normalized_query:
        like = f"%{normalized_query}%"
        where.append(
            """(
              object_type LIKE ?
              OR object_id LIKE ?
              OR release_id LIKE ?
              OR schema_version LIKE ?
              OR source_path LIKE ?
              OR summary_json LIKE ?
              OR raw_json LIKE ?
            )"""
        )
        params.extend([like, like, like, like, like, like, like])

    where_sql = ""
    if where:
        where_sql = "WHERE " + " AND ".join(where)

    safe_limit = max(1, min(int(limit), 500))

    rows = conn.execute(
        f"""
        SELECT object_type, object_id, release_id, schema_version,
               source_path, source_mtime, content_sha256, generated_at,
               imported_at, summary_json, raw_json
        FROM evidence_objects
        {where_sql}
        ORDER BY imported_at DESC, object_type ASC, object_id ASC
        LIMIT ?
        """,
        (*params, safe_limit),
    ).fetchall()

    items = []
    for row in rows:
        item = normalize_object_row(row, include_raw)
        if item is not None:
            items.append(item)

    return {
        "schemaVersion": "evidence.store.search/v1alpha1",
        "generatedAt": now_iso(),
        "count": len(items),
        "limit": safe_limit,
        "filters": {
            "query": normalized_query,
            "objectType": object_type,
            "releaseId": release_id,
            "includeRaw": include_raw,
        },
        "items": items,
    }


def query_verification_summary(
    conn: sqlite3.Connection,
    release_id: str | None = None,
    limit: int = 50,
) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    where = []
    params: list[Any] = []

    if release_id:
        where.append("release_id = ?")
        params.append(release_id)

    where_sql = ""
    if where:
        where_sql = "WHERE " + " AND ".join(where)

    safe_limit = max(1, min(int(limit), 500))

    rows = conn.execute(
        f"""
        SELECT object_type, object_id, release_id, imported_at, summary_json
        FROM evidence_objects
        {where_sql}
        ORDER BY release_id DESC, imported_at DESC, object_type ASC, object_id ASC
        LIMIT 5000
        """,
        tuple(params),
    ).fetchall()

    items = []
    for row in rows:
        summary = parse_json_field(row["summary_json"])
        verification = as_dict(summary.get("verification"))
        if not verification:
            continue

        items.append({
            "releaseId": row["release_id"],
            "objectType": row["object_type"],
            "objectId": row["object_id"],
            "importedAt": row["imported_at"],
            "verification": verification,
            "verificationMode": verification.get("mode"),
            "verificationStatus": verification.get("verificationStatus"),
            "verificationTool": verification.get("tool"),
            "verificationToolAvailable": verification.get("toolAvailable"),
            "signatureVerified": verification.get("signatureVerified"),
            "sbomPresent": verification.get("sbomPresent"),
            "provenancePresent": verification.get("provenancePresent"),
            "externalVerificationRequested": verification.get("externalVerificationRequested"),
            "externalVerificationAllowed": verification.get("externalVerificationAllowed"),
            "externalVerificationExecuted": verification.get("externalVerificationExecuted"),
            "externalVerificationSucceeded": verification.get("externalVerificationSucceeded"),
            "externalVerificationSkippedReason": verification.get("externalVerificationSkippedReason"),
            "canRunExternalVerification": verification.get("canRunExternalVerification"),
            "doesNotRunExternalCommands": verification.get("doesNotRunExternalCommands"),
        })

        if len(items) >= safe_limit:
            break

    return {
        "schemaVersion": "evidence.store.verificationSummary/v1alpha1",
        "generatedAt": now_iso(),
        "count": len(items),
        "limit": safe_limit,
        "filters": {
            "releaseId": release_id,
        },
        "latest": items[0] if items else None,
        "items": items,
    }


def evidence_object_node_id(object_type: str, object_id: str) -> str:
    return f"object:{object_type}:{object_id}"


def evidence_artifact_node_id(release_id: str, artifact_kind: str, path: str) -> str:
    return f"artifact:{release_id}:{artifact_kind}:{sha256_text(path)[:16]}"


def evidence_source_object_node_id(object_pk: str | None) -> str | None:
    if not object_pk:
        return None

    parts = str(object_pk).split(":", 2)
    if len(parts) != 3:
        return None

    object_type, _, object_id = parts
    return evidence_object_node_id(object_type, object_id)


def query_evidence_graph(conn: sqlite3.Connection, release_id: str) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    release = normalize_release_row(
        conn.execute(
            "SELECT * FROM releases WHERE release_id = ?",
            (release_id,),
        ).fetchone()
    )

    if release is None:
        raise SystemExit(f"ERROR: release not found: {release_id}")

    object_rows = conn.execute(
        """
        SELECT object_type, object_id, release_id, schema_version,
               source_path, source_mtime, content_sha256, generated_at,
               imported_at, summary_json, raw_json
        FROM evidence_objects
        WHERE release_id = ?
        ORDER BY object_type, object_id
        """,
        (release_id,),
    ).fetchall()

    artifact_rows = conn.execute(
        """
        SELECT release_id, artifact_kind, path, exists_flag, content_type,
               size_bytes, modified_at, source_object_pk
        FROM release_artifacts
        WHERE release_id = ?
        ORDER BY artifact_kind, path
        """,
        (release_id,),
    ).fetchall()

    verification_summary = query_verification_summary(conn, release_id=release_id, limit=50)

    nodes: list[dict[str, Any]] = []
    edges: list[dict[str, Any]] = []

    release_node_id = f"release:{release_id}"
    nodes.append({
        "id": release_node_id,
        "type": "release",
        "label": release_id,
        "releaseId": release_id,
        "data": release,
    })

    object_nodes_by_key: dict[tuple[str, str], str] = {}

    for row in object_rows:
        item = normalize_object_row(row, include_raw=False)
        if item is None:
            continue

        object_type = str(item.get("object_type") or "")
        object_id = str(item.get("object_id") or "")
        node_id = evidence_object_node_id(object_type, object_id)
        object_nodes_by_key[(object_type, object_id)] = node_id

        nodes.append({
            "id": node_id,
            "type": "evidenceObject",
            "label": object_type,
            "releaseId": release_id,
            "objectType": object_type,
            "objectId": object_id,
            "schemaVersion": item.get("schema_version"),
            "data": item,
        })
        edges.append({
            "from": release_node_id,
            "to": node_id,
            "type": "containsEvidenceObject",
        })

    for row in artifact_rows:
        item = row_to_dict(row) or {}
        if item.get("exists_flag") is not None:
            item["exists"] = bool(item.pop("exists_flag"))
        else:
            item.pop("exists_flag", None)

        artifact_kind = str(item.get("artifact_kind") or "")
        artifact_path = str(item.get("path") or "")
        node_id = evidence_artifact_node_id(release_id, artifact_kind, artifact_path)

        nodes.append({
            "id": node_id,
            "type": "artifact",
            "label": artifact_kind,
            "releaseId": release_id,
            "artifactKind": artifact_kind,
            "path": artifact_path,
            "data": item,
        })
        edges.append({
            "from": release_node_id,
            "to": node_id,
            "type": "hasArtifact",
        })

        source_node_id = evidence_source_object_node_id(item.get("source_object_pk"))
        if source_node_id:
            edges.append({
                "from": source_node_id,
                "to": node_id,
                "type": "emitsArtifact",
            })

    for item in verification_summary.get("items", []):
        object_type = str(item.get("objectType") or "")
        object_id = str(item.get("objectId") or "")
        source_node_id = object_nodes_by_key.get((object_type, object_id), evidence_object_node_id(object_type, object_id))
        node_id = f"verification:{release_id}:{object_type}:{object_id}"

        nodes.append({
            "id": node_id,
            "type": "verificationSummary",
            "label": item.get("verificationMode") or "verification",
            "releaseId": release_id,
            "objectType": object_type,
            "objectId": object_id,
            "data": item,
        })
        edges.append({
            "from": source_node_id,
            "to": node_id,
            "type": "hasVerificationSummary",
        })
        edges.append({
            "from": release_node_id,
            "to": node_id,
            "type": "hasVerificationSummary",
        })

    return {
        "schemaVersion": "evidence.store.graph/v1alpha1",
        "generatedAt": now_iso(),
        "releaseId": release_id,
        "release": release,
        "objectCount": len(object_rows),
        "artifactCount": len(artifact_rows),
        "verificationSummary": verification_summary,
        "nodeCount": len(nodes),
        "edgeCount": len(edges),
        "nodes": nodes,
        "edges": edges,
    }

def query_release(conn: sqlite3.Connection, release_id: str, include_raw: bool) -> dict[str, Any]:
    conn.row_factory = sqlite3.Row

    release = row_to_dict(
        conn.execute(
            "SELECT * FROM releases WHERE release_id = ?",
            (release_id,),
        ).fetchone()
    )

    if release is None:
        raise SystemExit(f"ERROR: release not found: {release_id}")

    object_rows = conn.execute(
        """
        SELECT object_type, object_id, release_id, schema_version,
               source_path, source_mtime, content_sha256, generated_at,
               imported_at, summary_json, raw_json
        FROM evidence_objects
        WHERE release_id = ?
        ORDER BY object_type, object_id
        """,
        (release_id,),
    ).fetchall()

    objects = []
    for row in object_rows:
        item = normalize_object_row(row, include_raw)
        if item is not None:
            objects.append(item)

    artifact_rows = conn.execute(
        """
        SELECT artifact_kind, path, exists_flag, content_type, size_bytes,
               modified_at, source_object_pk
        FROM release_artifacts
        WHERE release_id = ?
        ORDER BY artifact_kind, path
        """,
        (release_id,),
    ).fetchall()

    artifacts = []
    for row in artifact_rows:
        item = row_to_dict(row) or {}
        if item.get("exists_flag") is not None:
            item["exists"] = bool(item.pop("exists_flag"))
        else:
            item.pop("exists_flag", None)
        artifacts.append(item)

    return {
        "schemaVersion": "evidence.store.release/v1alpha1",
        "generatedAt": now_iso(),
        "release": release,
        "objectCount": len(objects),
        "objects": objects,
        "artifactCount": len(artifacts),
        "artifacts": artifacts,
    }


def open_db(path: Path) -> sqlite3.Connection:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(path))
    conn.row_factory = sqlite3.Row
    return conn


def main() -> int:
    parser = argparse.ArgumentParser(
        description="S Sentinel EvidenceStore SQLite utility."
    )
    sub = parser.add_subparsers(dest="command", required=True)

    init_parser = sub.add_parser("init-db", help="Initialize SQLite EvidenceStore DB.")
    init_parser.add_argument("--db", required=True)

    schema_parser = sub.add_parser("schema", help="Show SQLite EvidenceStore schema and migration state.")
    schema_parser.add_argument("--db", required=True)

    import_parser = sub.add_parser("import-dir", help="Import report JSON files into SQLite.")
    import_parser.add_argument("--db", required=True)
    import_parser.add_argument("--report-dir", required=True)

    list_parser = sub.add_parser("list-releases", help="List releases from SQLite.")
    list_parser.add_argument("--db", required=True)
    list_parser.add_argument("--limit", type=int, default=50)
    list_parser.add_argument("--service")
    list_parser.add_argument("--env")
    list_parser.add_argument("--release-result")

    query_parser = sub.add_parser("query-release", help="Query one release from SQLite.")
    query_parser.add_argument("--db", required=True)
    query_parser.add_argument("--release-id", required=True)
    query_parser.add_argument("--include-raw", action="store_true")

    object_parser = sub.add_parser("get-object", help="Get one evidence object from SQLite.")
    object_parser.add_argument("--db", required=True)
    object_parser.add_argument("--object-type", required=True)
    object_parser.add_argument("--object-id", required=True)
    object_parser.add_argument("--release-id")
    object_parser.add_argument("--include-raw", action="store_true")

    artifacts_parser = sub.add_parser("list-artifacts", help="List release artifacts from SQLite.")
    artifacts_parser.add_argument("--db", required=True)
    artifacts_parser.add_argument("--limit", type=int, default=50)
    artifacts_parser.add_argument("--release-id")
    artifacts_parser.add_argument("--artifact-kind")

    search_parser = sub.add_parser("search-objects", help="Search evidence objects from SQLite.")
    search_parser.add_argument("--db", required=True)
    search_parser.add_argument("--query", default="")
    search_parser.add_argument("--limit", type=int, default=50)
    search_parser.add_argument("--object-type")
    search_parser.add_argument("--release-id")
    search_parser.add_argument("--include-raw", action="store_true")

    verification_parser = sub.add_parser("verification-summary", help="Query compact verification summaries.")
    verification_parser.add_argument("--db", required=True)
    verification_parser.add_argument("--release-id")
    verification_parser.add_argument("--limit", type=int, default=50)

    graph_parser = sub.add_parser("graph", help="Query release evidence graph.")
    graph_parser.add_argument("--db", required=True)
    graph_parser.add_argument("--release-id", required=True)

    args = parser.parse_args()

    db_path = Path(args.db)

    with open_db(db_path) as conn:
        if args.command == "init-db":
            init_db(conn)
            print(json.dumps({
                "schemaVersion": "evidence.store.init/v1alpha1",
                "db": str(db_path),
                "status": "initialized",
                "storeSchema": schema_state(conn),
            }, ensure_ascii=False, indent=2))
            return 0

        if args.command == "schema":
            init_db(conn)
            result = schema_state(conn)
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "import-dir":
            result = import_dir(conn, Path(args.report_dir))
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "list-releases":
            result = list_releases(
                conn,
                args.limit,
                service=args.service,
                env=args.env,
                release_result=args.release_result,
            )
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "query-release":
            result = query_release(conn, args.release_id, args.include_raw)
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "get-object":
            result = get_object(
                conn,
                args.object_type,
                args.object_id,
                args.release_id,
                args.include_raw,
            )
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "list-artifacts":
            result = list_artifacts(
                conn,
                args.limit,
                release_id=args.release_id,
                artifact_kind=args.artifact_kind,
            )
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "search-objects":
            result = search_objects(
                conn,
                args.query,
                args.limit,
                object_type=args.object_type,
                release_id=args.release_id,
                include_raw=args.include_raw,
            )
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "verification-summary":
            result = query_verification_summary(
                conn,
                release_id=args.release_id,
                limit=args.limit,
            )
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

        if args.command == "graph":
            result = query_evidence_graph(conn, args.release_id)
            result["db"] = str(db_path)
            print(json.dumps(result, ensure_ascii=False, indent=2))
            return 0

    return 1


if __name__ == "__main__":
    raise SystemExit(main())
