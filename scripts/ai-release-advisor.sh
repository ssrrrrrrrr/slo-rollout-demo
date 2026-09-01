#!/bin/bash
set -euo pipefail

REPORT_FILE="${1:-}"
CONTEXT_FILE="${RELEASE_CONTEXT_FILE:-}"
OLLAMA_URL="${OLLAMA_URL:-http://127.0.0.1:11434}"
MODEL="${MODEL:-qwen2.5:0.5b}"
OLLAMA_TIMEOUT_SECONDS="${OLLAMA_TIMEOUT_SECONDS:-30}"
OLLAMA_NUM_CTX="${OLLAMA_NUM_CTX:-1024}"
OLLAMA_NUM_PREDICT="${OLLAMA_NUM_PREDICT:-512}"
ADVISOR_REPORT_TEXT_LIMIT="${ADVISOR_REPORT_TEXT_LIMIT:-4000}"
OUT_DIR="${AI_ADVICE_OUTPUT_DIR:-docs/release-reports}"
TS="$(date +%Y%m%d-%H%M%S)"

if [ -z "$REPORT_FILE" ]; then
  REPORT_FILE="$(ls -t docs/release-reports/release-report-*.md 2>/dev/null | head -n 1 || true)"
fi

if [ -z "$CONTEXT_FILE" ] && [ -n "$REPORT_FILE" ] && [ -f "$REPORT_FILE" ]; then
  CONTEXT_FILE="$(awk -F'|' '/Release Context File/ {gsub(/^[ \\t]+|[ \\t]+$/, "", $3); print $3; exit}' "$REPORT_FILE" 2>/dev/null || true)"

  if [ "$CONTEXT_FILE" = "not provided" ]; then
    CONTEXT_FILE=""
  fi
fi

if [ -z "$REPORT_FILE" ] || [ ! -f "$REPORT_FILE" ]; then
  echo "ERROR: release report file not found" >&2
  exit 1
fi

if [ -n "$CONTEXT_FILE" ] && [ ! -f "$CONTEXT_FILE" ]; then
  RAW_CONTEXT_FILE="$CONTEXT_FILE"
  CONTEXT_BASENAME="$(basename "$RAW_CONTEXT_FILE")"
  REPORT_PARENT_DIR="$(cd "$(dirname "$REPORT_FILE")" && pwd)"

  for candidate in \
    "$RAW_CONTEXT_FILE" \
    "$REPORT_PARENT_DIR/$CONTEXT_BASENAME" \
    "$(pwd)/$RAW_CONTEXT_FILE" \
    "/app/$RAW_CONTEXT_FILE" \
    "/app/docs/release-reports/$CONTEXT_BASENAME" \
    "/data/nfs/slo-rollout-watcher/reports/$CONTEXT_BASENAME"; do
    if [ -f "$candidate" ]; then
      CONTEXT_FILE="$candidate"
      break
    fi
  done
fi

if [ -z "$CONTEXT_FILE" ] || [ ! -f "$CONTEXT_FILE" ]; then
  echo "ERROR: release context file not found: ${CONTEXT_FILE:-not provided}" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
OUT="${OUT_DIR}/ai-advice-${TS}.md"
DECISION_OUT="${OUT_DIR}/ai-decision-${TS}.json"


SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

validate_generated_release_contract() {
  local contract_file="${1:-}"
  local helper="${RELEASE_CONTRACT_VALIDATOR_HELPER:-$SCRIPT_DIR/validate-generated-release-contract.sh}"

  if [ "${RELEASE_CONTRACT_VALIDATION_MODE:-warn}" = "off" ]; then
    return 0
  fi

  if [ -f "$helper" ]; then
    bash "$helper" "$contract_file"
  else
    echo "WARN: release contract validator helper not found: $helper" >&2
  fi
}

python3 - "$OLLAMA_URL" "$MODEL" "$OLLAMA_TIMEOUT_SECONDS" "$OLLAMA_NUM_CTX" "$OLLAMA_NUM_PREDICT" "$ADVISOR_REPORT_TEXT_LIMIT" "$REPORT_FILE" "$CONTEXT_FILE" "$OUT" "$DECISION_OUT" <<'PY'
import json
import sys
import urllib.request
from pathlib import Path

try:
    import yaml
except Exception:
    yaml = None

ollama_url = sys.argv[1].rstrip("/")
model = sys.argv[2]
ollama_timeout_seconds = int(sys.argv[3])
ollama_num_ctx = int(sys.argv[4])
ollama_num_predict = int(sys.argv[5])
advisor_report_text_limit = int(sys.argv[6])
report_file = Path(sys.argv[7])
context_file = Path(sys.argv[8])
out_file = Path(sys.argv[9])
decision_out_file = Path(sys.argv[10])

ctx = json.loads(context_file.read_text(encoding="utf-8"))
report_text = report_file.read_text(encoding="utf-8", errors="ignore")[:advisor_report_text_limit]

def resolve_existing_path(raw):
    if not raw:
        return None

    candidate = Path(str(raw))
    if candidate.is_absolute():
        return candidate if candidate.exists() else None

    search_roots = [
        Path.cwd(),
        context_file.parent,
        report_file.parent,
        out_file.parent,
    ]

    for root in search_roots:
        resolved = root / candidate
        if resolved.exists():
            return resolved

    return None

def read_yaml_object(path):
    if not path or not path.exists() or yaml is None:
        return None
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
        return data if isinstance(data, dict) else None
    except Exception:
        return None

service = ctx.get("service") or ctx.get("rollout") or "unknown"
env = ctx.get("env") or "unknown"
slo_id = ctx.get("sloId")
slo_config_ref = ctx.get("sloConfigRef")
slo_config_path = resolve_existing_path(slo_config_ref)
slo_config_snapshot = read_yaml_object(slo_config_path)

strategy_id = ctx.get("strategyId")
strategy_config_ref = ctx.get("strategyConfigRef")
strategy_config_path = resolve_existing_path(strategy_config_ref)
strategy_config_snapshot = read_yaml_object(strategy_config_path)

slo_objectives = []
if isinstance(slo_config_snapshot, dict):
    spec = slo_config_snapshot.get("spec") or {}
    objectives = spec.get("objectives") or []
    if isinstance(objectives, list):
        slo_objectives = objectives

strategy_type = None
strategy_traffic_steps = []
strategy_failure_policy = {}
strategy_promotion_policy = {}

if isinstance(strategy_config_snapshot, dict):
    strategy_spec = strategy_config_snapshot.get("spec") or {}
    strategy_type = strategy_spec.get("strategyType")

    traffic = strategy_spec.get("traffic") or {}
    steps = traffic.get("steps") or []
    if isinstance(steps, list):
        strategy_traffic_steps = steps

    failure_policy = strategy_spec.get("failurePolicy") or {}
    if isinstance(failure_policy, dict):
        strategy_failure_policy = failure_policy

    promotion_policy = strategy_spec.get("promotionPolicy") or {}
    if isinstance(promotion_policy, dict):
        strategy_promotion_policy = promotion_policy

def arr(v):
    if v is None:
        return []
    if isinstance(v, list):
        return v
    return [v]

def bullet(items):
    items = [str(x) for x in items if x not in (None, "")]
    if not items:
        return "- 无"
    return "\n".join(f"- {x}" for x in items)

def j(obj):
    return json.dumps(obj, ensure_ascii=False, indent=2)

change = ctx.get("changeContext") or {}
image = change.get("image") or {}
env_changes = change.get("envChanges") or []
slo_changes = change.get("sloGateChanges") or {}

failed_metrics = arr(ctx.get("failedMetrics"))
release_result = ctx.get("result") or "UNKNOWN"
risk_reasons = arr(ctx.get("riskReasons"))
change_hints = arr(ctx.get("changeRiskHints"))

rollout_phase = ctx.get("rolloutPhase")
rollout_abort = ctx.get("rolloutAbort")
analysis_phase = ctx.get("analysisRunPhase")
severity = ctx.get("severity")
risk_score = ctx.get("riskScore")
change_risk_level = ctx.get("changeRiskLevel")
change_risk_score = ctx.get("changeRiskScore")

conclusion = "本次发布需要人工介入。"

if release_result == "PASS":
    conclusion = "本次发布已通过 SLO 门禁，当前可以继续观察或进入后续发布阶段。"
elif release_result == "IN_PROGRESS":
    conclusion = "本次发布仍在进行中，需要继续观察 Rollout 和 AnalysisRun 状态。"
elif release_result == "FAIL_BY_REQUEST_COUNT":
    conclusion = "本次发布失败主要表现为 request-count 样本不足，需要先补充流量再重试，不应直接判断为代码故障。"
elif release_result == "FAIL_BY_ERROR_RATE":
    conclusion = "本次发布因 error-rate 门禁失败，说明 canary 版本 5xx 错误比例超过阈值，不建议继续 promote。"
elif release_result == "FAIL_BY_P95_LATENCY":
    conclusion = "本次发布因 p95-latency 门禁失败，说明 canary 版本尾延迟超过阈值，不建议继续 promote。"
elif release_result == "FAIL_BY_MULTIPLE_SLO":
    conclusion = "本次发布存在多个 SLO 门禁失败，应停止发布并优先定位 canary 版本质量问题。"
elif release_result == "FAIL_BY_ROLLOUT_ABORT":
    conclusion = "本次发布已经被 Rollout 中止，不建议继续 promote。"
elif release_result == "FAIL_BY_ROLLOUT_DEGRADED":
    conclusion = "本次发布对应 Rollout 已进入 Degraded 状态，需要人工介入排查。"
elif rollout_phase == "Degraded" or rollout_abort is True or analysis_phase == "Failed":
    conclusion = "本次发布失败或已被中止，不建议继续 promote。"

if change_risk_level in ("high", "critical") and release_result in ("FAIL_BY_ERROR_RATE", "FAIL_BY_P95_LATENCY", "FAIL_BY_MULTIPLE_SLO"):
    conclusion = "本次发布属于高风险变更导致质量回退的可能性较高，应停止发布并回滚或修复后重新发布。"

deterministic = f"""# AI Release Advisor

## 1. 结论

{conclusion}

## 2. 本次变更摘要

- Change Risk Level: {change_risk_level}
- Change Risk Score: {change_risk_score}
- Change Risk Hints:
{bullet(change_hints)}

### Image Change

{j(image)}

### Environment Changes

{j(env_changes)}

### SLO Gate Changes

{j(slo_changes)}

## 2.1 SLO-as-Code 输入

- Service: {service}
- Env: {env}
- SLO ID: {slo_id}
- SLO Config Ref: {slo_config_ref}
- SLO Config Loaded: {slo_config_snapshot is not None}

### SLO Objectives

{j(slo_objectives)}

## 2.2 Progressive Delivery Strategy-as-Code 输入

- Strategy ID: {strategy_id}
- Strategy Config Ref: {strategy_config_ref}
- Strategy Config Loaded: {strategy_config_snapshot is not None}
- Strategy Type: {strategy_type}
- Auto Promotion Enabled: {strategy_promotion_policy.get("autoPromotionEnabled")}
- Requires Human Approval: {strategy_promotion_policy.get("requiresHumanApproval")}
- On SLO Failure: {strategy_failure_policy.get("onSLOFailure")}
- Rollback Allowed: {strategy_failure_policy.get("rollbackAllowed")}

### Strategy Traffic Steps

{j(strategy_traffic_steps)}

### Strategy Failure Policy

{j(strategy_failure_policy)}

### Strategy Promotion Policy

{j(strategy_promotion_policy)}

## 3. 发布失败证据

- Namespace: {ctx.get("namespace")}
- Rollout: {ctx.get("rollout")}
- Rollout Phase: {rollout_phase}
- Rollout Abort: {rollout_abort}
- AnalysisRun: {ctx.get("analysisRun")}
- AnalysisRun Phase: {analysis_phase}
- Release Result: {release_result}
- Failed Metrics: {", ".join(failed_metrics) if failed_metrics else "无"}
- Release Severity: {severity}
- Release Risk Score: {risk_score}

### Release Risk Reasons

{bullet(risk_reasons)}

## 4. 变更与失败指标的关联分析

"""

if change_risk_level in ("high", "critical"):
    deterministic += "本次变更风险较高，需要优先检查 ChangeContext 中的高风险项。\n\n"
else:
    deterministic += "本次变更风险不高，需要结合 AnalysisRun 失败指标继续判断。\n\n"

if release_result and release_result != "UNKNOWN":
    deterministic += f"- 平台标准发布结果为 `{release_result}`，AI 分析应以该结构化结果为第一判断依据。\n"

if any(x.get("name") == "FAULT_RATE" for x in env_changes):
    deterministic += "- FAULT_RATE 发生变化，若 failedMetrics 包含 error-rate，则说明新版本错误率升高与本次变更存在较强关联。\n"
if any(x.get("name") == "LATENCY_MS" for x in env_changes):
    deterministic += "- LATENCY_MS 发生变化，若 failedMetrics 包含 p95-latency，则说明新版本延迟升高与本次变更存在较强关联。\n"
if image.get("changed"):
    deterministic += "- 镜像 tag 已变化，说明本次确实引入了新的应用版本。\n"
if "request-count" in failed_metrics and len(failed_metrics) == 1:
    deterministic += "- 当前仅 request-count 失败，更可能是灰度阶段请求样本不足，而不是新版本质量问题。\n"
if "error-rate" in failed_metrics:
    deterministic += "- error-rate 失败，说明 canary 版本 5xx 错误比例超过门禁阈值。\n"
if "p95-latency" in failed_metrics:
    deterministic += "- p95-latency 失败，说明 canary 版本尾延迟超过门禁阈值。\n"

deterministic += f"""

## 5. 影响范围

异常发生在 Argo Rollouts 灰度发布阶段。由于 Rollout 已经进入 {rollout_phase}，并且 abort={rollout_abort}，坏版本不会继续扩大到全量发布。

## 6. 建议动作

- 不要强行跳过 SLO 门禁。
- 保持 Rollout 中止状态，先定位 canary 版本问题。
- 优先检查本次变更项，尤其是 FAULT_RATE、LATENCY_MS、镜像 tag 和业务代码变更。
- 如果只是 request-count 样本不足，应补充流量后重试发布。
- 如果 error-rate 或 p95-latency 失败，应回滚或修复后使用新 tag 重新发布。
"""

system_prompt = "你是云原生发布可靠性分析助手。请基于 ReleaseContext、ChangeContext 和发布报告，用中文补充分析。不要编造不存在的数据。"
user_prompt = f"""
请基于以下确定性分析继续补充，但不要覆盖它。

确定性分析：
{deterministic}

ReleaseContext JSON：
{j(ctx)}

SLOConfig Snapshot JSON：
{j(slo_config_snapshot)}

StrategyConfig Snapshot JSON：
{j(strategy_config_snapshot)}

发布报告摘录：
{report_text}
"""

llm_text = ""
try:
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
        "stream": False,
        "options": {
            "num_ctx": ollama_num_ctx,
            "num_predict": ollama_num_predict,
        },
    }
    req = urllib.request.Request(
        f"{ollama_url}/api/chat",
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=ollama_timeout_seconds) as resp:
        data = json.loads(resp.read().decode("utf-8"))
        llm_text = data.get("message", {}).get("content", "").strip()
except Exception as e:
    llm_text = f"Ollama 调用失败，已使用确定性规则生成报告。错误：{e}"

final = f"""<!--
Generated by ai-release-advisor.sh
Source context: {context_file}
Source report: {report_file}
Model: {model}
Ollama URL: {ollama_url}
Ollama Timeout Seconds: {ollama_timeout_seconds}
Ollama Num Ctx: {ollama_num_ctx}
Ollama Num Predict: {ollama_num_predict}
Advisor Report Text Limit: {advisor_report_text_limit}
Change Risk Level: {change_risk_level}
Change Risk Score: {change_risk_score}
-->

{deterministic}

## 7. LLM 补充分析

{llm_text}
"""

def bool_from_result(value):
    return bool(value)

requires_human_approval = release_result.startswith("FAIL_") or release_result == "UNKNOWN"
safe_to_retry = release_result in ("PASS", "FAIL_BY_REQUEST_COUNT", "IN_PROGRESS")

if release_result in ("FAIL_BY_ERROR_RATE", "FAIL_BY_P95_LATENCY", "FAIL_BY_MULTIPLE_SLO"):
    safe_to_retry = False

summary = conclusion

decision_source = "deterministic_rule"
confidence = "high"

if release_result == "UNKNOWN":
    decision_source = "insufficient_context"
    confidence = "low"

policy_hints = []

if release_result == "PASS":
    policy_hints.extend([
        "release_result_passed",
        "no_human_approval_required",
        "safe_to_continue",
    ])
elif release_result == "IN_PROGRESS":
    policy_hints.extend([
        "release_in_progress",
        "continue_observing",
        "no_human_approval_required",
    ])
elif release_result == "FAIL_BY_REQUEST_COUNT":
    policy_hints.extend([
        "request_count_insufficient",
        "retry_with_more_traffic_allowed",
        "no_human_approval_required",
    ])
elif release_result == "FAIL_BY_MULTIPLE_SLO":
    policy_hints.extend([
        "multiple_slo_gates_failed",
        "human_approval_required",
        "unsafe_to_retry_without_fix",
        "stop_promotion_recommended",
    ])
elif release_result in ("FAIL_BY_ERROR_RATE", "FAIL_BY_P95_LATENCY"):
    policy_hints.extend([
        "single_slo_gate_failed",
        "human_approval_required",
        "unsafe_to_retry_without_fix",
        "stop_promotion_recommended",
    ])
elif release_result in ("FAIL_BY_ROLLOUT_ABORT", "FAIL_BY_ROLLOUT_DEGRADED"):
    policy_hints.extend([
        "rollout_unhealthy",
        "human_approval_required",
        "investigation_required",
    ])
else:
    policy_hints.extend([
        "insufficient_context",
        "manual_review_required",
    ])

if requires_human_approval and "human_approval_required" not in policy_hints:
    policy_hints.append("human_approval_required")

if not safe_to_retry and "unsafe_to_retry_without_fix" not in policy_hints:
    policy_hints.append("unsafe_to_retry_without_fix")

agent_action = {
    "type": "MANUAL_REVIEW",
    "allowed": False,
    "requiresApproval": True,
    "reason": "Release result is unknown and requires manual review",
}

if release_result == "PASS":
    agent_action = {
        "type": "NOOP",
        "allowed": True,
        "requiresApproval": False,
        "reason": "Release passed all SLO gates",
    }
elif release_result == "IN_PROGRESS":
    agent_action = {
        "type": "OBSERVE",
        "allowed": True,
        "requiresApproval": False,
        "reason": "Release is still in progress",
    }
elif release_result == "FAIL_BY_REQUEST_COUNT":
    agent_action = {
        "type": "RETRY_WITH_MORE_TRAFFIC",
        "allowed": True,
        "requiresApproval": False,
        "reason": "Canary traffic sample is insufficient",
    }
elif release_result in ("FAIL_BY_ERROR_RATE", "FAIL_BY_P95_LATENCY", "FAIL_BY_MULTIPLE_SLO"):
    agent_action = {
        "type": "STOP_PROMOTION",
        "allowed": True,
        "requiresApproval": True,
        "reason": "Release failed SLO gates and requires human investigation",
    }
elif release_result in ("FAIL_BY_ROLLOUT_ABORT", "FAIL_BY_ROLLOUT_DEGRADED"):
    agent_action = {
        "type": "INVESTIGATE",
        "allowed": True,
        "requiresApproval": True,
        "reason": "Rollout is aborted or degraded and requires investigation",
    }

execution_mode = "advisory_only"

next_steps = []
if release_result == "PASS":
    next_steps = [
        "archive_release_record",
        "continue_observing",
    ]
elif release_result == "IN_PROGRESS":
    next_steps = [
        "continue_observing_rollout",
        "wait_for_analysisrun_result",
    ]
elif release_result == "FAIL_BY_REQUEST_COUNT":
    next_steps = [
        "continue_generating_canary_traffic",
        "rerun_release_with_sufficient_request_count",
    ]
elif release_result in ("FAIL_BY_ERROR_RATE", "FAIL_BY_P95_LATENCY", "FAIL_BY_MULTIPLE_SLO"):
    next_steps = [
        "stop_promotion",
        "inspect_canary_logs",
        "compare_change_context",
        "publish_fixed_version_with_new_tag",
    ]
elif release_result in ("FAIL_BY_ROLLOUT_ABORT", "FAIL_BY_ROLLOUT_DEGRADED"):
    next_steps = [
        "inspect_rollout_events",
        "inspect_analysisrun_details",
        "keep_promotion_stopped_until_review",
    ]
else:
    next_steps = [
        "manual_review_release_context",
        "inspect_rollout_and_analysisrun",
    ]

guardrails = {
    "autoExecute": False,
    "executionMode": execution_mode,
    "requiresGitOpsChange": True,
    "requiresHumanApprovalForRiskyAction": True,
    "allowedActions": [
        "NOOP",
        "OBSERVE",
        "RETRY_WITH_MORE_TRAFFIC",
        "STOP_PROMOTION",
        "INVESTIGATE",
        "MANUAL_REVIEW",
    ],
    "blockedActions": [
        "ROLLBACK",
        "PROMOTE",
        "DELETE_RESOURCE",
        "PATCH_RESOURCE",
        "APPLY_MANIFEST",
    ],
}

evidence = {
    "service": service,
    "env": env,
    "sloId": slo_id,
    "sloConfigRef": slo_config_ref,
    "sloObjectives": slo_objectives,
    "strategyId": strategy_id,
    "strategyConfigRef": strategy_config_ref,
    "strategyType": strategy_type,
    "strategyTrafficSteps": strategy_traffic_steps,
    "strategyFailurePolicy": strategy_failure_policy,
    "strategyPromotionPolicy": strategy_promotion_policy,
    "failedMetrics": failed_metrics,
    "rolloutPhase": rollout_phase,
    "rolloutAbort": rollout_abort,
    "analysisRunPhase": analysis_phase,
    "riskLevel": severity,
    "riskScore": risk_score,
    "riskReasons": risk_reasons,
    "changeRiskLevel": change_risk_level,
    "changeRiskScore": change_risk_score,
}

decision_json = {
    "schemaVersion": "ai.release.advisor/v1alpha1",
    "generatedBy": "ai-release-advisor.sh",
    "model": model,
    "releaseResult": release_result,
    "decisionSource": decision_source,
    "confidence": confidence,
    "executionMode": execution_mode,
    "summary": summary,
    "conclusion": conclusion,
    "failedMetrics": failed_metrics,
    "riskLevel": severity,
    "riskScore": risk_score,
    "riskReasons": risk_reasons,
    "changeRiskLevel": change_risk_level,
    "changeRiskScore": change_risk_score,
    "changeRiskHints": change_hints,
    "decision": ctx.get("decision"),
    "recommendedAction": ctx.get("recommendedAction"),
    "requiresHumanApproval": requires_human_approval,
    "safeToRetry": safe_to_retry,
    "service": service,
    "env": env,
    "sloId": slo_id,
    "sloConfigRef": slo_config_ref,
    "sloConfigSnapshot": slo_config_snapshot,
    "sloObjectives": slo_objectives,
    "strategyId": strategy_id,
    "strategyConfigRef": strategy_config_ref,
    "strategyConfigSnapshot": strategy_config_snapshot,
    "strategyType": strategy_type,
    "strategyTrafficSteps": strategy_traffic_steps,
    "strategyFailurePolicy": strategy_failure_policy,
    "strategyPromotionPolicy": strategy_promotion_policy,
    "policyHints": policy_hints,
    "agentAction": agent_action,
    "guardrails": guardrails,
    "evidence": evidence,
    "nextSteps": next_steps,
    "rollout": {
        "namespace": ctx.get("namespace"),
        "name": ctx.get("rollout"),
        "phase": rollout_phase,
        "abort": rollout_abort,
        "message": ctx.get("rolloutMessage"),
        "stableReplicaSet": ctx.get("stableReplicaSet"),
        "currentDesiredVersion": ctx.get("currentDesiredVersion"),
    },
    "analysisRun": {
        "name": ctx.get("analysisRun"),
        "phase": analysis_phase,
        "metrics": ctx.get("analysisRunMetrics") or [],
    },
    "sources": {
        "releaseContext": str(context_file),
        "releaseReport": str(report_file),
        "aiAdvice": str(out_file),
        "sloConfig": str(slo_config_path) if slo_config_path else slo_config_ref,
        "strategyConfig": str(strategy_config_path) if strategy_config_path else strategy_config_ref,
    },
}

out_file.write_text(final, encoding="utf-8")
decision_out_file.write_text(json.dumps(decision_json, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(f"AI advisor report generated: {out_file}")
print(f"AI decision generated: {decision_out_file}")
print(f"Source context: {context_file}")
print(f"Source report: {report_file}")
print(f"Change risk: {change_risk_level} score={change_risk_score}")
PY

validate_generated_release_contract "$DECISION_OUT"

POLICY_EVALUATOR=""
POLICY_EVALUATOR_MODE="legacy"
POLICY_RUNTIME_ADAPTER=""

if [ -x "./scripts/policy-runtime-adapter.py" ]; then
  POLICY_RUNTIME_ADAPTER="./scripts/policy-runtime-adapter.py"
elif [ -x "/app/scripts/policy-runtime-adapter.py" ]; then
  POLICY_RUNTIME_ADAPTER="/app/scripts/policy-runtime-adapter.py"
fi

if [ -n "$POLICY_RUNTIME_ADAPTER" ]; then
  POLICY_EVALUATOR="$POLICY_RUNTIME_ADAPTER"
  POLICY_EVALUATOR_MODE="runtime-adapter"
elif [ -x "./scripts/evaluate-agent-decision.sh" ]; then
  POLICY_EVALUATOR="./scripts/evaluate-agent-decision.sh"
elif [ -x "/app/scripts/evaluate-agent-decision.sh" ]; then
  POLICY_EVALUATOR="/app/scripts/evaluate-agent-decision.sh"
fi

if [ -n "$POLICY_EVALUATOR" ]; then
  echo "Running policy evaluator: $POLICY_EVALUATOR"
  DECISION_BASENAME="$(basename "$DECISION_OUT")"
  DECISION_SUFFIX="${DECISION_BASENAME#ai-decision-}"
  POLICY_OUT="${OUT_DIR}/policy-decision-${DECISION_SUFFIX}"

  if [ "$POLICY_EVALUATOR_MODE" = "runtime-adapter" ]; then
    POLICY_INPUT_OUT="${OUT_DIR}/policy-input-${DECISION_SUFFIX}"
    POLICY_RUNTIME_OUT="${OUT_DIR}/policy-runtime-result-${DECISION_SUFFIX}"

    "$POLICY_EVALUATOR" build-input \
      --ai-decision "$DECISION_OUT" \
      --policy-file "${RELEASE_POLICY_FILE:-policy/release-policy.yaml}" \
      --output "$POLICY_INPUT_OUT"

    "$POLICY_EVALUATOR" evaluate \
      --runtime "${POLICY_RUNTIME:-local-python}" \
      --policy-input "$POLICY_INPUT_OUT" \
      --output "$POLICY_RUNTIME_OUT" \
      --repo-dir "$(pwd)" \
      --decision-output "$POLICY_OUT"
  else
    "$POLICY_EVALUATOR" "$DECISION_OUT"
  fi

  if [ -f "$POLICY_OUT" ]; then
    EVIDENCE_BUILDER=""

    if [ -x "./scripts/build-release-evidence.sh" ]; then
      EVIDENCE_BUILDER="./scripts/build-release-evidence.sh"
    elif [ -x "/app/scripts/build-release-evidence.sh" ]; then
      EVIDENCE_BUILDER="/app/scripts/build-release-evidence.sh"
    fi

    if [ -n "$EVIDENCE_BUILDER" ]; then
      echo "Running release evidence builder: $EVIDENCE_BUILDER"
      RELEASE_CONTRACT_VALIDATION_MODE=off "$EVIDENCE_BUILDER" "$DECISION_OUT" "$POLICY_OUT"

      EVIDENCE_OUT="${OUT_DIR}/release-evidence-${DECISION_SUFFIX}"
      SUMMARY_BUILDER=""

      if [ -x "./scripts/build-release-summary.sh" ]; then
        SUMMARY_BUILDER="./scripts/build-release-summary.sh"
      elif [ -x "/app/scripts/build-release-summary.sh" ]; then
        SUMMARY_BUILDER="/app/scripts/build-release-summary.sh"
      fi

      if [ -n "$SUMMARY_BUILDER" ] && [ -f "$EVIDENCE_OUT" ]; then
        echo "Running release summary builder: $SUMMARY_BUILDER"
        "$SUMMARY_BUILDER" "$EVIDENCE_OUT"
      elif [ -z "$SUMMARY_BUILDER" ]; then
        echo "WARN: release summary builder not found, skip release summary generation" >&2
      else
        echo "WARN: expected release evidence file not found: $EVIDENCE_OUT" >&2
      fi

      FAILURE_EVIDENCE_COLLECTOR=""

      if [ -x "./scripts/collect-failure-evidence.sh" ]; then
        FAILURE_EVIDENCE_COLLECTOR="./scripts/collect-failure-evidence.sh"
      elif [ -x "/app/scripts/collect-failure-evidence.sh" ]; then
        FAILURE_EVIDENCE_COLLECTOR="/app/scripts/collect-failure-evidence.sh"
      fi

      SHOULD_COLLECT_FAILURE_EVIDENCE="$(python3 -c 'import json, sys; from pathlib import Path; ai=json.loads(Path(sys.argv[1]).read_text(encoding="utf-8")); policy=json.loads(Path(sys.argv[2]).read_text(encoding="utf-8")); rr=str(ai.get("releaseResult","UNKNOWN")); fa=str(policy.get("finalAction","UNKNOWN")); rha=bool(policy.get("requiresHumanApproval", False)); print("true" if rr.startswith("FAIL_") or fa == "STOP_PROMOTION" or (rha and rr != "PASS") else "false")' "$DECISION_OUT" "$POLICY_OUT")"

      if [ "$SHOULD_COLLECT_FAILURE_EVIDENCE" = "true" ]; then
        if [ -n "$FAILURE_EVIDENCE_COLLECTOR" ] && [ -n "$CONTEXT_FILE" ] && [ -f "$CONTEXT_FILE" ]; then
          echo "Running failure evidence collector: $FAILURE_EVIDENCE_COLLECTOR"
          RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR="${FAILURE_EVIDENCE_OUTPUT_DIR:-$OUT_DIR}"

          FAILURE_EVIDENCE_OUTPUT_DIR="$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR" \
          COLLECT_K8S_EVIDENCE="${COLLECT_K8S_EVIDENCE:-false}" \
            "$FAILURE_EVIDENCE_COLLECTOR" "$CONTEXT_FILE" || {
              echo "WARN: collect-failure-evidence.sh failed, continue release advice pipeline" >&2
            }

          FAILURE_EVIDENCE_JSON="$(ls -t "$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR"/failure-evidence-*.json 2>/dev/null | grep -v 'failure-evidence-latest.json' | head -1 || true)"
          FAILURE_EVIDENCE_MD="$(ls -t "$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR"/failure-evidence-*.md 2>/dev/null | grep -v 'failure-evidence-latest.md' | head -1 || true)"

          if [ -z "$FAILURE_EVIDENCE_JSON" ] && [ -f "$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR/failure-evidence-latest.json" ]; then
            FAILURE_EVIDENCE_JSON="$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR/failure-evidence-latest.json"
          fi

          if [ -z "$FAILURE_EVIDENCE_MD" ] && [ -f "$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR/failure-evidence-latest.md" ]; then
            FAILURE_EVIDENCE_MD="$RESOLVED_FAILURE_EVIDENCE_OUTPUT_DIR/failure-evidence-latest.md"
          fi

          if [ -f "$EVIDENCE_OUT" ] && [ -n "$FAILURE_EVIDENCE_JSON" ] && [ -n "$FAILURE_EVIDENCE_MD" ]; then
            echo "Linking failure evidence into release evidence: $EVIDENCE_OUT"
            python3 - "$EVIDENCE_OUT" "$FAILURE_EVIDENCE_JSON" "$FAILURE_EVIDENCE_MD" <<'LINK_FAILURE_EVIDENCE_PY'
import json
import sys
from pathlib import Path

release_evidence_path = Path(sys.argv[1])
failure_json = Path(sys.argv[2])
failure_md = Path(sys.argv[3])

data = json.loads(release_evidence_path.read_text(encoding="utf-8"))
artifacts = data.setdefault("artifacts", {})
artifacts["failureEvidence"] = str(failure_json)
artifacts["failureEvidenceReport"] = str(failure_md)

data.setdefault("failureEvidenceRef", {
    "generated": True,
    "json": str(failure_json),
    "markdown": str(failure_md),
})

release_evidence_path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(f"Failure evidence linked into release evidence: {release_evidence_path}")
LINK_FAILURE_EVIDENCE_PY
          else
            echo "WARN: failure evidence files not found or release evidence missing, skip linking failure evidence" >&2
          fi
        elif [ -z "$FAILURE_EVIDENCE_COLLECTOR" ]; then
          echo "WARN: failure evidence collector not found, skip failure evidence generation" >&2
        else
          echo "WARN: release context file not found, skip failure evidence generation: ${CONTEXT_FILE:-not provided}" >&2
        fi
      else
        echo "Failure evidence collector skipped: release is not a failure"
      fi

      ACTION_PLAN_BUILDER=""

      if [ -x "./scripts/build-action-plan.sh" ]; then
        ACTION_PLAN_BUILDER="./scripts/build-action-plan.sh"
      elif [ -x "/app/scripts/build-action-plan.sh" ]; then
        ACTION_PLAN_BUILDER="/app/scripts/build-action-plan.sh"
      fi

      if [ -n "$ACTION_PLAN_BUILDER" ] && [ -f "$EVIDENCE_OUT" ]; then
        echo "Running action plan builder: $ACTION_PLAN_BUILDER"
        RESOLVED_ACTION_PLAN_OUTPUT_DIR="${ACTION_PLAN_OUTPUT_DIR:-$OUT_DIR}"

        ACTION_PLAN_OUTPUT_DIR="$RESOLVED_ACTION_PLAN_OUTPUT_DIR" \
          "$ACTION_PLAN_BUILDER" "$EVIDENCE_OUT" || {
            echo "WARN: build-action-plan.sh failed, continue release advice pipeline" >&2
          }

        ACTION_PLAN_JSON="$RESOLVED_ACTION_PLAN_OUTPUT_DIR/action-plan-${DECISION_SUFFIX}"
        ACTION_PLAN_MD="$RESOLVED_ACTION_PLAN_OUTPUT_DIR/action-plan-${DECISION_SUFFIX%.json}.md"

        if [ ! -f "$ACTION_PLAN_JSON" ] && [ -f "$RESOLVED_ACTION_PLAN_OUTPUT_DIR/action-plan-latest.json" ]; then
          ACTION_PLAN_JSON="$RESOLVED_ACTION_PLAN_OUTPUT_DIR/action-plan-latest.json"
        fi

        if [ ! -f "$ACTION_PLAN_MD" ] && [ -f "$RESOLVED_ACTION_PLAN_OUTPUT_DIR/action-plan-latest.md" ]; then
          ACTION_PLAN_MD="$RESOLVED_ACTION_PLAN_OUTPUT_DIR/action-plan-latest.md"
        fi

        if [ -f "$EVIDENCE_OUT" ] && [ -f "$ACTION_PLAN_JSON" ] && [ -f "$ACTION_PLAN_MD" ]; then
          echo "Linking action plan into release evidence: $EVIDENCE_OUT"
          python3 - "$EVIDENCE_OUT" "$ACTION_PLAN_JSON" "$ACTION_PLAN_MD" <<'LINK_ACTION_PLAN_PY'
import json
import sys
from pathlib import Path

release_evidence_path = Path(sys.argv[1])
action_plan_json = Path(sys.argv[2])
action_plan_md = Path(sys.argv[3])

data = json.loads(release_evidence_path.read_text(encoding="utf-8"))
artifacts = data.setdefault("artifacts", {})
artifacts["actionPlan"] = str(action_plan_json)
artifacts["actionPlanReport"] = str(action_plan_md)

data["actionPlanRef"] = {
    "generated": True,
    "json": str(action_plan_json),
    "markdown": str(action_plan_md),
    "executionMode": "dry_run",
    "willExecute": False
}

release_evidence_path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(f"Action plan linked into release evidence: {release_evidence_path}")
LINK_ACTION_PLAN_PY
        else
          echo "WARN: action plan files not found or release evidence missing, skip linking action plan" >&2
        fi
      elif [ -z "$ACTION_PLAN_BUILDER" ]; then
        echo "WARN: action plan builder not found, skip action plan generation" >&2
      else
        echo "WARN: expected release evidence file not found, skip action plan generation: $EVIDENCE_OUT" >&2
      fi

      SUPPLY_CHAIN_DECISION_BUILDER=""

      if [ -x "./scripts/build-supply-chain-decision.sh" ]; then
        SUPPLY_CHAIN_DECISION_BUILDER="./scripts/build-supply-chain-decision.sh"
      elif [ -x "/app/scripts/build-supply-chain-decision.sh" ]; then
        SUPPLY_CHAIN_DECISION_BUILDER="/app/scripts/build-supply-chain-decision.sh"
      fi

      if [ -n "$SUPPLY_CHAIN_DECISION_BUILDER" ] && [ -f "$EVIDENCE_OUT" ]; then
        echo "Running supply-chain safety decision builder: $SUPPLY_CHAIN_DECISION_BUILDER"
        RESOLVED_SUPPLY_CHAIN_DECISION_OUTPUT_DIR="${SUPPLY_CHAIN_DECISION_OUTPUT_DIR:-$OUT_DIR}"

        if RELEASE_REPORT_DIR="$OUT_DIR" \
          SUPPLY_CHAIN_DECISION_OUTPUT_DIR="$RESOLVED_SUPPLY_CHAIN_DECISION_OUTPUT_DIR" \
          "$SUPPLY_CHAIN_DECISION_BUILDER" "$EVIDENCE_OUT"; then
          SUPPLY_CHAIN_DECISION_JSON="$RESOLVED_SUPPLY_CHAIN_DECISION_OUTPUT_DIR/supply-chain-decision-${DECISION_SUFFIX}"

          if [ ! -f "$SUPPLY_CHAIN_DECISION_JSON" ] && [ -f "$RESOLVED_SUPPLY_CHAIN_DECISION_OUTPUT_DIR/supply-chain-decision-latest.json" ]; then
            SUPPLY_CHAIN_DECISION_JSON="$RESOLVED_SUPPLY_CHAIN_DECISION_OUTPUT_DIR/supply-chain-decision-latest.json"
          fi

          SIGNED_RELEASE_GATE_BUILDER=""

          if [ -x "./scripts/build-signed-release-gate.sh" ]; then
            SIGNED_RELEASE_GATE_BUILDER="./scripts/build-signed-release-gate.sh"
          elif [ -x "/app/scripts/build-signed-release-gate.sh" ]; then
            SIGNED_RELEASE_GATE_BUILDER="/app/scripts/build-signed-release-gate.sh"
          fi

          if [ -n "$SIGNED_RELEASE_GATE_BUILDER" ] && [ -f "$SUPPLY_CHAIN_DECISION_JSON" ]; then
            echo "Running signed release gate builder: $SIGNED_RELEASE_GATE_BUILDER"
            RESOLVED_SIGNED_RELEASE_GATE_OUTPUT_DIR="${SIGNED_RELEASE_GATE_OUTPUT_DIR:-$OUT_DIR}"

            RELEASE_REPORT_DIR="$OUT_DIR" \
            SIGNED_RELEASE_GATE_OUTPUT_DIR="$RESOLVED_SIGNED_RELEASE_GATE_OUTPUT_DIR" \
              "$SIGNED_RELEASE_GATE_BUILDER" "$SUPPLY_CHAIN_DECISION_JSON" || {
                echo "WARN: build-signed-release-gate.sh failed, continue release advice pipeline" >&2
              }
          elif [ -z "$SIGNED_RELEASE_GATE_BUILDER" ]; then
            echo "WARN: signed release gate builder not found, skip signed release gate" >&2
          else
            echo "WARN: expected supply-chain decision file not found, skip signed release gate: $SUPPLY_CHAIN_DECISION_JSON" >&2
          fi

          validate_generated_release_contract "$EVIDENCE_OUT"
        else
          echo "WARN: build-supply-chain-decision.sh failed, continue release advice pipeline" >&2
        fi
      elif [ -z "$SUPPLY_CHAIN_DECISION_BUILDER" ]; then
        echo "WARN: supply-chain decision builder not found, skip supply-chain safety check" >&2
      else
        echo "WARN: expected release evidence file not found, skip supply-chain safety check: $EVIDENCE_OUT" >&2
      fi

      AGENT_TRACE_BUILDER=""

      if [ -x "./scripts/build-agent-trace.sh" ]; then
        AGENT_TRACE_BUILDER="./scripts/build-agent-trace.sh"
      elif [ -x "/app/scripts/build-agent-trace.sh" ]; then
        AGENT_TRACE_BUILDER="/app/scripts/build-agent-trace.sh"
      fi

      if [ -n "$AGENT_TRACE_BUILDER" ] && [ -f "$DECISION_OUT" ]; then
        echo "Running agent trace builder: $AGENT_TRACE_BUILDER"
        RESOLVED_AGENT_TRACE_OUTPUT_DIR="${AGENT_TRACE_OUTPUT_DIR:-$OUT_DIR}"

        RELEASE_REPORT_DIR="$OUT_DIR" \
        AGENT_TRACE_OUTPUT_DIR="$RESOLVED_AGENT_TRACE_OUTPUT_DIR" \
          "$AGENT_TRACE_BUILDER" "$DECISION_OUT" || {
            echo "WARN: build-agent-trace.sh failed, continue release advice pipeline" >&2
          }
      elif [ -z "$AGENT_TRACE_BUILDER" ]; then
        echo "WARN: agent trace builder not found, skip agent trace generation" >&2
      else
        echo "WARN: expected AI decision file not found, skip agent trace generation: $DECISION_OUT" >&2
      fi

      RELEASE_MEMORY_BUILDER=""

      if [ -x "./scripts/build-release-memory.sh" ]; then
        RELEASE_MEMORY_BUILDER="./scripts/build-release-memory.sh"
      elif [ -x "/app/scripts/build-release-memory.sh" ]; then
        RELEASE_MEMORY_BUILDER="/app/scripts/build-release-memory.sh"
      fi

      if [ -n "$RELEASE_MEMORY_BUILDER" ] && [ -f "$EVIDENCE_OUT" ]; then
        echo "Running release memory builder: $RELEASE_MEMORY_BUILDER"

        RELEASE_REPORT_DIR="$OUT_DIR" \
          "$RELEASE_MEMORY_BUILDER" || {
            echo "WARN: build-release-memory.sh failed, continue release advice pipeline" >&2
          }
      elif [ -z "$RELEASE_MEMORY_BUILDER" ]; then
        echo "WARN: release memory builder not found, skip release memory generation" >&2
      else
        echo "WARN: expected release evidence file not found, skip release memory generation: $EVIDENCE_OUT" >&2
      fi

      RELEASE_INTELLIGENCE_BUILDER=""

      if [ -x "./scripts/build-release-intelligence.sh" ]; then
        RELEASE_INTELLIGENCE_BUILDER="./scripts/build-release-intelligence.sh"
      elif [ -x "/app/scripts/build-release-intelligence.sh" ]; then
        RELEASE_INTELLIGENCE_BUILDER="/app/scripts/build-release-intelligence.sh"
      fi

      if [ -n "$RELEASE_INTELLIGENCE_BUILDER" ] && [ -f "$EVIDENCE_OUT" ]; then
        echo "Running release intelligence builder: $RELEASE_INTELLIGENCE_BUILDER"
        RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR="${RELEASE_INTELLIGENCE_OUTPUT_DIR:-$OUT_DIR}"

        RELEASE_REPORT_DIR="$OUT_DIR" \
        RELEASE_MEMORY_FILE="${RELEASE_MEMORY_FILE:-$OUT_DIR/release-memory.jsonl}" \
        RELEASE_INTELLIGENCE_OUTPUT_DIR="$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR" \
          "$RELEASE_INTELLIGENCE_BUILDER" "$EVIDENCE_OUT" || {
            echo "WARN: build-release-intelligence.sh failed, continue release advice pipeline" >&2
          }

        RELEASE_INTELLIGENCE_JSON="$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR/release-intelligence-${DECISION_SUFFIX}"
        RELEASE_INTELLIGENCE_MD="$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR/release-intelligence-${DECISION_SUFFIX%.json}.md"

        if [ ! -f "$RELEASE_INTELLIGENCE_JSON" ] && [ -f "$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR/release-intelligence-latest.json" ]; then
          RELEASE_INTELLIGENCE_JSON="$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR/release-intelligence-latest.json"
        fi

        if [ ! -f "$RELEASE_INTELLIGENCE_MD" ] && [ -f "$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR/release-intelligence-latest.md" ]; then
          RELEASE_INTELLIGENCE_MD="$RESOLVED_RELEASE_INTELLIGENCE_OUTPUT_DIR/release-intelligence-latest.md"
        fi

        if [ -f "$EVIDENCE_OUT" ] && [ -f "$RELEASE_INTELLIGENCE_JSON" ] && [ -f "$RELEASE_INTELLIGENCE_MD" ]; then
          echo "Linking release intelligence into release evidence: $EVIDENCE_OUT"
          python3 - "$EVIDENCE_OUT" "$RELEASE_INTELLIGENCE_JSON" "$RELEASE_INTELLIGENCE_MD" <<'LINK_RELEASE_INTELLIGENCE_PY'
import json
import sys
from pathlib import Path

release_evidence_path = Path(sys.argv[1])
release_intelligence_json = Path(sys.argv[2])
release_intelligence_md = Path(sys.argv[3])

data = json.loads(release_evidence_path.read_text(encoding="utf-8"))
artifacts = data.setdefault("artifacts", {})
artifacts["releaseIntelligence"] = str(release_intelligence_json)
artifacts["releaseIntelligenceReport"] = str(release_intelligence_md)

data["releaseIntelligenceRef"] = {
    "generated": True,
    "json": str(release_intelligence_json),
    "markdown": str(release_intelligence_md),
    "readOnlyAnalysis": True
}

release_evidence_path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(f"Release intelligence linked into release evidence: {release_evidence_path}")
LINK_RELEASE_INTELLIGENCE_PY

          if [ -n "${SUMMARY_BUILDER:-}" ] && [ -f "$EVIDENCE_OUT" ]; then
            echo "Rebuilding release summary with intelligence: $SUMMARY_BUILDER"
            "$SUMMARY_BUILDER" "$EVIDENCE_OUT" || {
              echo "WARN: rebuild release summary with intelligence failed, continue release advice pipeline" >&2
            }
          fi
        else
          echo "WARN: release intelligence files not found or release evidence missing, skip linking release intelligence" >&2
        fi
      elif [ -z "$RELEASE_INTELLIGENCE_BUILDER" ]; then
        echo "WARN: release intelligence builder not found, skip release intelligence generation" >&2
      else
        echo "WARN: expected release evidence file not found, skip release intelligence generation: $EVIDENCE_OUT" >&2
      fi

      if [ -f "$EVIDENCE_OUT" ]; then
        echo "Running final release evidence contract validation: $EVIDENCE_OUT"
        validate_generated_release_contract "$EVIDENCE_OUT"
      fi
    else
      echo "WARN: release evidence builder not found, skip release evidence bundle generation" >&2
    fi

    python3 - "$OUT" "$POLICY_OUT" <<'POLICY_SUMMARY_PY'
import json
import sys
from pathlib import Path

advice_file = Path(sys.argv[1])
policy_file = Path(sys.argv[2])

policy = json.loads(policy_file.read_text(encoding="utf-8"))

summary = f"""

## 8. Policy Evaluator Decision

- Policy Decision: `{policy.get("policyDecision", "UNKNOWN")}`
- Final Action: `{policy.get("finalAction", "UNKNOWN")}`
- Execution Mode: `{policy.get("executionMode", "unknown")}`
- Requires Human Approval: `{str(policy.get("requiresHumanApproval", False)).lower()}`
- Reason: {policy.get("reason", "not provided")}
- Policy Decision File: `{policy_file}`

### Matched Policy Rules

"""

for rule in policy.get("matchedRules", []):
    summary += f"- `{rule}`\n"

summary += "\n"

with advice_file.open("a", encoding="utf-8") as f:
    f.write(summary)

print(f"Policy summary appended to AI advice: {advice_file}")
POLICY_SUMMARY_PY

    if [ -f "${EVIDENCE_OUT:-}" ]; then
      python3 - "$OUT" "$EVIDENCE_OUT" <<'INTELLIGENCE_ADVICE_PY'
import json
import sys
from pathlib import Path

advice_file = Path(sys.argv[1])
evidence_file = Path(sys.argv[2])

try:
    evidence = json.loads(evidence_file.read_text(encoding="utf-8"))
except Exception:
    print(f"WARN: failed to read release evidence for intelligence summary: {evidence_file}", file=sys.stderr)
    raise SystemExit(0)

artifacts = evidence.get("artifacts") or {}
release_intelligence_ref = evidence.get("releaseIntelligenceRef") or {}

def resolve_artifact(ref):
    if not ref:
        return None

    p = Path(str(ref))
    candidates = []

    if p.is_absolute():
        candidates.append(p)

    candidates.append(evidence_file.parent / p.name)
    candidates.append(p)

    for candidate in candidates:
        if candidate.exists() and candidate.is_file():
            return candidate

    return None

intelligence_path = resolve_artifact(
    artifacts.get("releaseIntelligence") or release_intelligence_ref.get("json")
)

if not intelligence_path:
    print("WARN: release intelligence file not found, skip appending intelligence to AI advice", file=sys.stderr)
    raise SystemExit(0)

try:
    intelligence_doc = json.loads(intelligence_path.read_text(encoding="utf-8"))
except Exception:
    print(f"WARN: failed to parse release intelligence file: {intelligence_path}", file=sys.stderr)
    raise SystemExit(0)

current_text = advice_file.read_text(encoding="utf-8") if advice_file.exists() else ""
if "## 9. Release Intelligence Summary" in current_text:
    print(f"Release intelligence summary already exists in AI advice: {advice_file}")
    raise SystemExit(0)

intelligence = intelligence_doc.get("intelligence") or {}
history = intelligence_doc.get("history") or {}

risk_pattern = intelligence.get("riskPattern", "UNKNOWN")
repeated_risk_pattern = intelligence.get("repeatedRiskPattern", False)
recommended_next_action = intelligence.get("recommendedNextAction", "UNKNOWN")
conclusion = intelligence.get("humanSummary") or intelligence.get("conclusion") or "not provided"

similar_count = history.get("similarFailureCount", 0)
exact_count = history.get("exactHistoricalMetricSetMatchCount", 0)
similar_failures = history.get("similarFailures") or []

if similar_failures:
    similar_lines = []
    for item in similar_failures[:5]:
        metrics = ", ".join(item.get("failedMetrics") or [])
        similarity = item.get("similarity") or {}
        similar_lines.append(
            f"- `{item.get('releaseId', 'unknown')}` / `{item.get('appVersion', 'unknown')}` / "
            f"`{item.get('releaseResult', 'UNKNOWN')}` / Metrics=`{metrics or 'none'}` / "
            f"FinalAction=`{item.get('finalAction', 'UNKNOWN')}` / "
            f"ExactMatch=`{str(similarity.get('exactMetricSetMatch', False)).lower()}`"
        )
    similar_text = "\n".join(similar_lines)
else:
    similar_text = "未发现历史相似失败记录。"

intelligence_report = artifacts.get("releaseIntelligenceReport") or release_intelligence_ref.get("markdown") or "not provided"

section = f"""

## 9. Release Intelligence Summary

- Risk Pattern: `{risk_pattern}`
- Repeated Risk Pattern: `{str(repeated_risk_pattern).lower()}`
- Similar Historical Failure Count: `{similar_count}`
- Exact Historical Metric Set Match Count: `{exact_count}`
- Recommended Next Action: `{recommended_next_action}`

{conclusion}

### Similar Historical Failures

{similar_text}

### Release Intelligence Artifacts

- Release Intelligence JSON: `{intelligence_path}`
- Release Intelligence Report: `{intelligence_report}`

### Safety Boundary

This intelligence summary is read-only analysis. It does not execute Rollback, Promote, Patch, Delete, GitOps changes, image builds, commits, or pushes.
"""

with advice_file.open("a", encoding="utf-8") as f:
    f.write(section)

print(f"Release intelligence summary appended to AI advice: {advice_file}")
INTELLIGENCE_ADVICE_PY
    else
      echo "WARN: release evidence not found, skip release intelligence summary in AI advice: ${EVIDENCE_OUT:-not provided}" >&2
    fi
  else
    echo "WARN: expected policy decision file not found: $POLICY_OUT" >&2
  fi
else
  echo "WARN: policy evaluator not found, skip policy decision generation" >&2
fi

echo "===== build operator runbook and RCA ====="

if [ -f "${EVIDENCE_OUT:-}" ]; then
  REPORT_OUTPUT_DIR="$(dirname "$EVIDENCE_OUT")"

  RELEASE_RUNBOOK_BUILDER=""
  if [ -f "./scripts/build-release-runbook.sh" ]; then
    RELEASE_RUNBOOK_BUILDER="./scripts/build-release-runbook.sh"
  elif [ -f "/app/scripts/build-release-runbook.sh" ]; then
    RELEASE_RUNBOOK_BUILDER="/app/scripts/build-release-runbook.sh"
  fi

  if [ -n "$RELEASE_RUNBOOK_BUILDER" ]; then
    if RUNBOOK_OUTPUT_DIR="$REPORT_OUTPUT_DIR" RELEASE_REPORT_DIR="$REPORT_OUTPUT_DIR" bash "$RELEASE_RUNBOOK_BUILDER" "$EVIDENCE_OUT"; then
      echo "Release runbook generated from evidence: $EVIDENCE_OUT"
    else
      echo "WARN: build-release-runbook.sh failed, continue release advice pipeline" >&2
    fi
  else
    echo "WARN: build-release-runbook.sh not found, skip runbook generation" >&2
  fi

  RELEASE_RCA_BUILDER=""
  if [ -f "./scripts/build-release-rca.sh" ]; then
    RELEASE_RCA_BUILDER="./scripts/build-release-rca.sh"
  elif [ -f "/app/scripts/build-release-rca.sh" ]; then
    RELEASE_RCA_BUILDER="/app/scripts/build-release-rca.sh"
  fi

  if [ -n "$RELEASE_RCA_BUILDER" ]; then
    if RCA_OUTPUT_DIR="$REPORT_OUTPUT_DIR" RELEASE_REPORT_DIR="$REPORT_OUTPUT_DIR" bash "$RELEASE_RCA_BUILDER" "$EVIDENCE_OUT"; then
      echo "Release RCA generated from evidence: $EVIDENCE_OUT"
    else
      echo "WARN: build-release-rca.sh failed, continue release advice pipeline" >&2
    fi
  else
    echo "WARN: build-release-rca.sh not found, skip RCA generation" >&2
  fi
else
  echo "WARN: release evidence not found, skip runbook/RCA generation: ${EVIDENCE_OUT:-not provided}" >&2
fi

echo "===== build release timeline ====="

if [ -f "${EVIDENCE_OUT:-}" ]; then
  REPORT_OUTPUT_DIR="$(dirname "$EVIDENCE_OUT")"

  RELEASE_TIMELINE_BUILDER=""
  if [ -f "./scripts/build-release-timeline.sh" ]; then
    RELEASE_TIMELINE_BUILDER="./scripts/build-release-timeline.sh"
  elif [ -f "/app/scripts/build-release-timeline.sh" ]; then
    RELEASE_TIMELINE_BUILDER="/app/scripts/build-release-timeline.sh"
  fi

  if [ -n "$RELEASE_TIMELINE_BUILDER" ]; then
    if RELEASE_TIMELINE_OUTPUT_DIR="$REPORT_OUTPUT_DIR" RELEASE_REPORT_DIR="$REPORT_OUTPUT_DIR" bash "$RELEASE_TIMELINE_BUILDER" "$EVIDENCE_OUT"; then
      echo "Release timeline generated from evidence: $EVIDENCE_OUT"
    else
      echo "WARN: build-release-timeline.sh failed, continue release advice pipeline" >&2
    fi
  else
    echo "WARN: build-release-timeline.sh not found, skip release timeline generation" >&2
  fi
else
  echo "WARN: release evidence not found, skip release timeline generation: ${EVIDENCE_OUT:-not provided}" >&2
fi


echo "===== build read-only agent run ====="

if [ -n "${EVIDENCE_OUT:-}" ] && [ -f "$EVIDENCE_OUT" ]; then
  AGENT_RUN_BUILDER=""

  if [ -x "./scripts/build-agent-run.sh" ]; then
    AGENT_RUN_BUILDER="./scripts/build-agent-run.sh"
  elif [ -x "/app/scripts/build-agent-run.sh" ]; then
    AGENT_RUN_BUILDER="/app/scripts/build-agent-run.sh"
  fi

  if [ -n "$AGENT_RUN_BUILDER" ]; then
    echo "Running read-only agent run builder: $AGENT_RUN_BUILDER"
    if RELEASE_REPORT_DIR="$OUT_DIR" "$AGENT_RUN_BUILDER" "$EVIDENCE_OUT"; then
      true
    else
      echo "WARN: build-agent-run.sh failed, continue release advice pipeline" >&2
    fi
  else
    echo "WARN: build-agent-run.sh not found, skip read-only agent run generation" >&2
  fi
else
  echo "WARN: release evidence not found, skip read-only agent run generation" >&2
fi

echo "===== build read-only plan run ====="

if [ -n "${EVIDENCE_OUT:-}" ] && [ -f "$EVIDENCE_OUT" ]; then
  PLAN_RUN_BUILDER=""

  if [ -x "./scripts/build-plan-run.sh" ]; then
    PLAN_RUN_BUILDER="./scripts/build-plan-run.sh"
  elif [ -x "/app/scripts/build-plan-run.sh" ]; then
    PLAN_RUN_BUILDER="/app/scripts/build-plan-run.sh"
  fi

  if [ -n "$PLAN_RUN_BUILDER" ]; then
    REPORT_OUTPUT_DIR="$(dirname "$EVIDENCE_OUT")"
    EVIDENCE_BASENAME="$(basename "$EVIDENCE_OUT")"
    EVIDENCE_SUFFIX="${EVIDENCE_BASENAME#release-evidence-}"
    AGENT_RUN_JSON="$REPORT_OUTPUT_DIR/agent-run-$EVIDENCE_SUFFIX"

    if [ -f "$AGENT_RUN_JSON" ]; then
      echo "Running read-only plan run builder: $PLAN_RUN_BUILDER"
      if RELEASE_REPORT_DIR="$REPORT_OUTPUT_DIR" \
        RELEASE_MEMORY_FILE="${RELEASE_MEMORY_FILE:-$REPORT_OUTPUT_DIR/release-memory.jsonl}" \
        PLAN_RUN_OUTPUT_DIR="${PLAN_RUN_OUTPUT_DIR:-$REPORT_OUTPUT_DIR}" \
        "$PLAN_RUN_BUILDER" "$AGENT_RUN_JSON" "$EVIDENCE_OUT"; then
        true
      else
        echo "WARN: build-plan-run.sh failed, continue release advice pipeline" >&2
      fi
    else
      echo "WARN: expected agent run file not found, skip plan run generation: $AGENT_RUN_JSON" >&2
    fi
  else
    echo "WARN: build-plan-run.sh not found, skip read-only plan run generation" >&2
  fi
else
  echo "WARN: release evidence not found, skip read-only plan run generation" >&2
fi

echo "===== build policy-bound execution request ====="

if [ -n "${EVIDENCE_OUT:-}" ] && [ -f "$EVIDENCE_OUT" ]; then
  EXECUTION_REQUEST_BUILDER=""

  if [ -x "./scripts/build-execution-request.sh" ]; then
    EXECUTION_REQUEST_BUILDER="./scripts/build-execution-request.sh"
  elif [ -x "/app/scripts/build-execution-request.sh" ]; then
    EXECUTION_REQUEST_BUILDER="/app/scripts/build-execution-request.sh"
  fi

  if [ -n "$EXECUTION_REQUEST_BUILDER" ]; then
    REPORT_OUTPUT_DIR="$(dirname "$EVIDENCE_OUT")"
    EVIDENCE_BASENAME="$(basename "$EVIDENCE_OUT")"
    EVIDENCE_SUFFIX="${EVIDENCE_BASENAME#release-evidence-}"
    PLAN_RUN_JSON="$REPORT_OUTPUT_DIR/plan-run-$EVIDENCE_SUFFIX"

    if [ -f "$PLAN_RUN_JSON" ]; then
      echo "Running policy-bound execution request builder: $EXECUTION_REQUEST_BUILDER"
      if RELEASE_REPORT_DIR="$REPORT_OUTPUT_DIR" \
        EXECUTION_REQUEST_OUTPUT_DIR="${EXECUTION_REQUEST_OUTPUT_DIR:-$REPORT_OUTPUT_DIR}" \
        REQUESTED_BY="${REQUESTED_BY:-read-only-agent-planner}" \
        "$EXECUTION_REQUEST_BUILDER" "$PLAN_RUN_JSON"; then
        validate_generated_release_contract "$EVIDENCE_OUT"
      else
        echo "WARN: build-execution-request.sh failed, continue release advice pipeline" >&2
      fi
    else
      echo "WARN: expected plan run file not found, skip execution request generation: $PLAN_RUN_JSON" >&2
    fi
  else
    echo "WARN: build-execution-request.sh not found, skip policy-bound execution request generation" >&2
  fi
else
  echo "WARN: release evidence not found, skip policy-bound execution request generation" >&2
fi

echo "===== build evidence control-plane record ====="

if [ -f "${EVIDENCE_OUT:-}" ]; then
  EVIDENCE_RECORD_BUILDER=""

  if [ -x "./scripts/build-evidence-record.sh" ]; then
    EVIDENCE_RECORD_BUILDER="./scripts/build-evidence-record.sh"
  elif [ -x "/app/scripts/build-evidence-record.sh" ]; then
    EVIDENCE_RECORD_BUILDER="/app/scripts/build-evidence-record.sh"
  fi

  if [ -n "$EVIDENCE_RECORD_BUILDER" ]; then
    REPORT_OUTPUT_DIR="$(dirname "$EVIDENCE_OUT")"
    RESOLVED_EVIDENCE_RECORD_OUTPUT_DIR="${EVIDENCE_RECORD_OUTPUT_DIR:-$REPORT_OUTPUT_DIR}"

    if EVIDENCE_RECORD_OUTPUT_DIR="$RESOLVED_EVIDENCE_RECORD_OUTPUT_DIR" \
      "$EVIDENCE_RECORD_BUILDER" "$EVIDENCE_OUT"; then

      EVIDENCE_BASENAME="$(basename "$EVIDENCE_OUT")"
      EVIDENCE_SUFFIX="${EVIDENCE_BASENAME#release-evidence-}"
      EVIDENCE_RECORD_JSON="$RESOLVED_EVIDENCE_RECORD_OUTPUT_DIR/evidence-record-$EVIDENCE_SUFFIX"

      if [ ! -f "$EVIDENCE_RECORD_JSON" ] && [ -f "$RESOLVED_EVIDENCE_RECORD_OUTPUT_DIR/evidence-record-latest.json" ]; then
        EVIDENCE_RECORD_JSON="$RESOLVED_EVIDENCE_RECORD_OUTPUT_DIR/evidence-record-latest.json"
      fi

      if [ -f "$EVIDENCE_RECORD_JSON" ]; then
        echo "Running evidence record contract validation: $EVIDENCE_RECORD_JSON"
        validate_generated_release_contract "$EVIDENCE_RECORD_JSON"
      else
        echo "WARN: expected evidence record file not found after builder run" >&2
      fi
    else
      echo "WARN: build-evidence-record.sh failed, continue release advice pipeline" >&2
    fi
  else
    echo "WARN: evidence record builder not found, skip evidence control-plane record generation" >&2
  fi
else
  echo "WARN: release evidence not found, skip evidence control-plane record generation: ${EVIDENCE_OUT:-not provided}" >&2
fi
