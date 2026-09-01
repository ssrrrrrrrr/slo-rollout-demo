#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ENV_NAME="dev"
SERVICE=""
IMAGE_TAG="v36-compiled"
APP_VERSION="v36"
FAULT_RATE="0"
LATENCY_MS="0"
OUTPUT_DIR="build/compiled"
COMPILER_PROFILE=""

REGISTRY="${REGISTRY:-registry.local:5000}"
IMAGE_NAME="${IMAGE_NAME:-}"
PROMETHEUS_ADDR="${PROMETHEUS_ADDR:-http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090}"
PROMETHEUS_RULE_NAMESPACE="${PROMETHEUS_RULE_NAMESPACE:-monitoring}"

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

usage() {
  cat <<'USAGE'
Usage:
  scripts/compile-release-config.sh [options]

Options:
  --env ENV                 Environment name. Default: dev
  --service NAME            Optional service override. Default: selected SLOConfig metadata.service
  --image-tag TAG           Release image tag. Default: v36-compiled
  --app-version VERSION     App version. Default: v36
  --fault-rate VALUE        Demo fault rate. Default: 0
  --latency-ms VALUE        Demo latency ms. Default: 0
  --output-dir DIR          Output root directory. Default: build/compiled
  --compiler-profile PATH   Optional CompilerProfile override. Default: EnvironmentConfig spec.compiler.defaultProfile
  -h, --help                Show help

Environment:
  REGISTRY                  Image registry. Default: registry.local:5000
  IMAGE_NAME                Optional image repository override. Default: SLOConfig spec.runtime.image.repository
  PYTHON_BIN                Python runtime. Default: python3, fallback: python
  PROMETHEUS_ADDR           Prometheus address used by AnalysisTemplate
  PROMETHEUS_RULE_NAMESPACE PrometheusRule namespace. Default: monitoring

Behavior:
  - Reads EnvironmentConfig, SLOConfig, and ProgressiveDeliveryStrategy.
  - Renders GitOps artifacts into build/compiled/<env>/ by default.
  - Does not apply Kubernetes resources.
  - Does not commit or push Git changes.
  - Does not build or push images.
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    --env)
      ENV_NAME="${2:?missing value for --env}"
      shift 2
      ;;
    --service)
      SERVICE="${2:?missing value for --service}"
      shift 2
      ;;
    --image-tag)
      IMAGE_TAG="${2:?missing value for --image-tag}"
      shift 2
      ;;
    --app-version)
      APP_VERSION="${2:?missing value for --app-version}"
      shift 2
      ;;
    --fault-rate)
      FAULT_RATE="${2:?missing value for --fault-rate}"
      shift 2
      ;;
    --latency-ms)
      LATENCY_MS="${2:?missing value for --latency-ms}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:?missing value for --output-dir}"
      shift 2
      ;;
    --compiler-profile)
      COMPILER_PROFILE="${2:?missing value for --compiler-profile}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

"$PYTHON_BIN" - "$ROOT_DIR" "$ENV_NAME" "$SERVICE" "$IMAGE_TAG" "$APP_VERSION" "$FAULT_RATE" "$LATENCY_MS" "$OUTPUT_DIR" "$REGISTRY" "$IMAGE_NAME" "$PROMETHEUS_ADDR" "$PROMETHEUS_RULE_NAMESPACE" "$COMPILER_PROFILE" <<'PY'
from __future__ import annotations

import json
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

try:
    import yaml
except Exception as exc:
    raise SystemExit(f"ERROR: PyYAML is required: {exc}")

root = Path(sys.argv[1])
env_name = sys.argv[2]
requested_service = sys.argv[3]
service = requested_service
image_tag = sys.argv[4]
app_version = sys.argv[5]
fault_rate = sys.argv[6]
latency_ms = sys.argv[7]
output_root = Path(sys.argv[8])
registry = sys.argv[9]
image_name = sys.argv[10]
prometheus_addr = sys.argv[11]
prometheus_rule_namespace = sys.argv[12]
compiler_profile_arg = sys.argv[13]

if not output_root.is_absolute():
    output_root = root / output_root

out_dir = output_root / env_name


class LiteralStr(str):
    pass


def literal_str_representer(dumper: yaml.Dumper, data: LiteralStr):
    return dumper.represent_scalar("tag:yaml.org,2002:str", data, style="|")


yaml.SafeDumper.add_representer(LiteralStr, literal_str_representer)


def read_yaml(rel_path: str) -> dict[str, Any]:
    path = root / rel_path
    if not path.is_file():
        raise SystemExit(f"ERROR: missing YAML file: {rel_path}")
    data = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise SystemExit(f"ERROR: YAML file must be an object: {rel_path}")
    return data


def write_yaml(path: Path, data: dict[str, Any]) -> None:
    path.write_text(
        yaml.safe_dump(data, sort_keys=False, allow_unicode=True),
        encoding="utf-8",
    )


def write_json(path: Path, data: dict[str, Any]) -> None:
    path.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def config_name(doc: dict[str, Any]) -> str:
    return str(doc.get("metadata", {}).get("name", ""))


def config_service(doc: dict[str, Any]) -> str:
    return str(doc.get("metadata", {}).get("service", ""))


def select_config(
    refs: list[str],
    default_profile: str | None,
    expected_kind: str,
) -> tuple[str, dict[str, Any]]:
    loaded: list[tuple[str, dict[str, Any]]] = []

    for ref in refs:
        doc = read_yaml(ref)
        if doc.get("kind") != expected_kind:
            continue
        loaded.append((ref, doc))

    for ref, doc in loaded:
        if default_profile and config_name(doc) == default_profile:
            return ref, doc

    for ref, doc in loaded:
        if config_service(doc) == service:
            return ref, doc

    if loaded:
        return loaded[0]

    raise SystemExit(f"ERROR: no {expected_kind} config found from refs={refs}")


def objective_by_id(slo_doc: dict[str, Any]) -> dict[str, dict[str, Any]]:
    objectives = slo_doc.get("spec", {}).get("objectives", [])
    if not isinstance(objectives, list):
        raise SystemExit("ERROR: spec.objectives must be a list in SLOConfig")
    result = {}
    for obj in objectives:
        obj_id = obj.get("id")
        if obj_id:
            result[str(obj_id)] = obj
    return result


def threshold(obj: dict[str, Any]) -> tuple[str, Any, str]:
    data = obj.get("threshold") or {}
    operator = str(data.get("operator", ""))
    value = data.get("value")
    unit = str(data.get("unit", ""))
    if not operator or value is None:
        raise SystemExit(f"ERROR: objective {obj.get('id')} missing threshold.operator/value")
    return operator, value, unit


def prom_window(slo_doc: dict[str, Any]) -> str:
    return str(slo_doc.get("spec", {}).get("evaluation", {}).get("window", "1m"))



def prometheus_bindings(slo_doc: dict[str, Any], metric_binding: dict[str, Any] | None = None) -> dict[str, Any]:
    prom = (
        slo_doc.get("spec", {})
        .get("observability", {})
        .get("prometheus", {})
    )
    metric_binding = metric_binding or {}
    profile_prom = metric_binding.get("prometheus") or {}

    labels = prom.get("labels") or {}
    profile_labels = profile_prom.get("labels") or {}

    return {
        "provider": str(metric_binding.get("provider") or "prometheus"),
        "bindingSource": str(metric_binding.get("bindingSource") or "SLOConfig.spec.observability.prometheus"),
        "requestCounter": str(profile_prom.get("requestCounter") or prom.get("requestCounter") or "demo_http_requests_total"),
        "latencyHistogram": str(profile_prom.get("latencyHistogram") or prom.get("latencyHistogram") or "demo_http_request_duration_seconds_bucket"),
        "errorStatusRegex": str(profile_prom.get("errorStatusRegex") or prom.get("errorStatusRegex") or "5.."),
        "labels": {
            "namespace": str(profile_labels.get("namespace") or labels.get("namespace") or "namespace"),
            "version": str(profile_labels.get("version") or labels.get("version") or "version"),
            "status": str(profile_labels.get("status") or labels.get("status") or "status"),
        },
    }

def prom_matchers(metric_bindings: dict[str, Any], namespace: str, version: str | None = None, status_regex: str | None = None) -> str:
    labels = metric_bindings.get("labels") or {}
    items = [f'{labels.get("namespace", "namespace")}="{namespace}"']

    if version is not None:
        items.append(f'{labels.get("version", "version")}="{version}"')

    if status_regex is not None:
        items.append(f'{labels.get("status", "status")}=~"{status_regex}"')

    return ",".join(items)

def prom_query(metric_id: str, obj: dict[str, Any], namespace: str, window: str, metric_bindings: dict[str, Any]) -> LiteralStr:
    obj_type = str(obj.get("type", ""))
    request_counter = metric_bindings["requestCounter"]
    latency_histogram = metric_bindings["latencyHistogram"]
    version_matchers = prom_matchers(metric_bindings, namespace, image_tag)
    error_matchers = prom_matchers(metric_bindings, namespace, image_tag, metric_bindings["errorStatusRegex"])

    if obj_type == "request_count":
        return LiteralStr(f'''(
  sum(increase({request_counter}{{{version_matchers}}}[{window}]))
  or on() vector(0)
)''')

    if obj_type == "error_rate":
        return LiteralStr(f'''(
  (
    sum(rate({request_counter}{{{error_matchers}}}[{window}]))
    or vector(0)
  )
  /
  clamp_min(
    (
      sum(rate({request_counter}{{{version_matchers}}}[{window}]))
      or vector(0)
    ),
    0.001
  )
) * 100''')

    if obj_type == "latency":
        percentile = float(obj.get("percentile", 95)) / 100
        return LiteralStr(f'''(
  histogram_quantile(
    {percentile:.2f},
    sum(rate({latency_histogram}{{{version_matchers}}}[{window}])) by (le)
  )
  or on() vector(0)
)''')

    raise SystemExit(f"ERROR: unsupported objective type for {metric_id}: {obj_type}")


def success_condition(metric_id: str, obj: dict[str, Any]) -> str:
    operator, value, _unit = threshold(obj)
    if str(obj.get("type")) == "latency":
        return f"isNaN(result[0]) || result[0] {operator} {value}"
    return f"result[0] {operator} {value}"


def alert_expr(metric_id: str, obj: dict[str, Any], namespace: str, min_request_count: Any, window: str, metric_bindings: dict[str, Any]):
    operator, value, _unit = threshold(obj)
    obj_type = str(obj.get("type", ""))
    request_counter = metric_bindings["requestCounter"]
    latency_histogram = metric_bindings["latencyHistogram"]
    namespace_matchers = prom_matchers(metric_bindings, namespace)
    error_matchers = prom_matchers(metric_bindings, namespace, status_regex=metric_bindings["errorStatusRegex"])

    if obj_type == "error_rate":
        return LiteralStr(f'''(
  (
    sum by (version) (
      rate({request_counter}{{{error_matchers}}}[{window}])
    )
    /
    clamp_min(
      sum by (version) (
        rate({request_counter}{{{namespace_matchers}}}[{window}])
      ),
      0.001
    )
  ) * 100 > {value}
)
and on(version)
(
  sum by (version) (
    increase({request_counter}{{{namespace_matchers}}}[{window}])
  ) >= {min_request_count}
)''')

    if obj_type == "latency":
        percentile = float(obj.get("percentile", 95)) / 100
        return LiteralStr(f'''(
  histogram_quantile(
    {percentile:.2f},
    sum by (version, le) (
      rate({latency_histogram}{{{namespace_matchers}}}[{window}])
    )
  ) > {value}
)
and on(version)
(
  sum by (version) (
    increase({request_counter}{{{namespace_matchers}}}[{window}])
  ) >= {min_request_count}
)''')

    return None


env_ref = f"configs/environments/{env_name}.yaml"
env_doc = read_yaml(env_ref)
env_spec = env_doc.get("spec") or {}

namespace = str(env_spec.get("kubernetes", {}).get("namespace") or "slo-rollout")
cluster_name = str(env_spec.get("cluster", {}).get("name") or "unknown")
environment_class = str(env_spec.get("cluster", {}).get("environmentClass") or "unknown")
policy_profile = str(env_spec.get("policies", {}).get("policyProfile") or "unknown")
project_name = str(env_spec.get("project", {}).get("name") or "slo-rollout-demo")
overlay_path = str(env_spec.get("gitops", {}).get("overlayPath") or f"deploy/overlays/{env_name}")

slo_refs = env_spec.get("slo", {}).get("configRefs") or []
strategy_refs = env_spec.get("strategy", {}).get("configRefs") or []

slo_ref, slo_doc = select_config(
    slo_refs,
    env_spec.get("slo", {}).get("defaultProfile"),
    "SLOConfig",
)

strategy_ref, strategy_doc = select_config(
    strategy_refs,
    env_spec.get("strategy", {}).get("defaultProfile"),
    "ProgressiveDeliveryStrategy",
)

service_source = "cli"
if not service:
    service = config_service(slo_doc)
    service_source = "sloConfig"

if not service:
    service = config_service(strategy_doc)
    service_source = "strategyConfig"

if not service:
    raise SystemExit("ERROR: service was not provided and selected configs do not define metadata.service")

strategy_service = config_service(strategy_doc)
if strategy_service and strategy_service != service:
    raise SystemExit(f"ERROR: strategy service mismatch: expected {service}, got {strategy_service}")

compiler_profile_refs = env_spec.get("compiler", {}).get("profileRefs") or []
compiler_default_profile = env_spec.get("compiler", {}).get("defaultProfile")
if compiler_profile_arg:
    compiler_profile_refs = [compiler_profile_arg]
    compiler_default_profile = None

compiler_profile_ref: str | None = None
compiler_profile_doc: dict[str, Any] = {}
compiler_profile_spec: dict[str, Any] = {}

if compiler_profile_refs:
    compiler_profile_ref, compiler_profile_doc = select_config(
        compiler_profile_refs,
        compiler_default_profile,
        "CompilerProfile",
    )
    compiler_profile_service = config_service(compiler_profile_doc)
    if compiler_profile_service and compiler_profile_service != service:
        raise SystemExit(
            f"ERROR: compiler profile service mismatch: expected {service}, got {compiler_profile_service}"
        )
    compiler_profile_spec = compiler_profile_doc.get("spec") or {}

service_config = compiler_profile_spec.get("serviceConfig") or {}
runtime_profile = compiler_profile_spec.get("runtimeProfile") or {}
health_config = service_config.get("health") or {}

container_name = str(service_config.get("containerName") or service)
service_port_name = str(service_config.get("servicePortName") or "http")
container_port = int(service_config.get("containerPort") or 8080)
readiness_path = str(health_config.get("readinessPath") or "/healthz")
liveness_path = str(health_config.get("livenessPath") or "/healthz")

replicas = int(runtime_profile.get("replicas") or 3)
revision_history_limit = int(runtime_profile.get("revisionHistoryLimit") or 3)
image_pull_policy = str(runtime_profile.get("imagePullPolicy") or "IfNotPresent")

renderer_refs = compiler_profile_spec.get("rendererRefs") or {}
rollout_template_renderer = str(renderer_refs.get("rolloutTemplate") or "argo-rollouts-canary-v1")
analysis_template_renderer = str(renderer_refs.get("analysisTemplateRenderer") or "prometheus-analysis-template-v1")
prometheus_rule_renderer = str(renderer_refs.get("prometheusRuleRenderer") or "prometheus-rule-v1")
environment_overlay_renderer = str(renderer_refs.get("environmentOverlayRenderer") or "kustomize-overlay-v1")

renderer_contracts = {
    "analysisTemplate": analysis_template_renderer,
    "rollout": rollout_template_renderer,
    "prometheusRule": prometheus_rule_renderer,
    "environmentOverlay": environment_overlay_renderer,
}

def renderer_annotations(renderer_ref: str, output_kind: str) -> dict[str, str]:
    data = {
        "ssentinel.io/generated-by": "config-compiler",
        "ssentinel.io/env": env_name,
        "ssentinel.io/renderer-ref": renderer_ref,
        "ssentinel.io/output-kind": output_kind,
    }
    if compiler_profile_ref:
        data["ssentinel.io/compiler-profile"] = str(config_name(compiler_profile_doc))
    return data

compiler_profile_guardrails = dict(compiler_profile_spec.get("guardrails") or {})
compiler_profile_guardrails.update({
    "profileModelOnly": False if compiler_profile_doc else True,
    "drivesRenderedWorkloadShape": bool(compiler_profile_doc),
    "doesNotApplyKubernetes": True,
    "doesNotCommitOrPush": True,
})

slo_spec = slo_doc.get("spec") or {}
runtime_spec = slo_spec.get("runtime") or {}
image_spec = runtime_spec.get("image") or {}
image_repository = str(image_name or image_spec.get("repository") or "sre/demo-app")
remote_image = f"{registry}/{image_repository}:{image_tag}"

strategy_spec = strategy_doc.get("spec") or {}
strategy_analysis = strategy_spec.get("analysis") or {}

objectives = objective_by_id(slo_doc)
metric_ids = strategy_analysis.get("metrics") or list(objectives.keys())
window = prom_window(slo_doc)
prometheus_metric_bindings = prometheus_bindings(slo_doc, compiler_profile_spec.get("metricBinding") or {})

min_request_count = (
    slo_spec.get("evaluation", {}).get("minRequestCount")
    or objectives.get("request-count", {}).get("threshold", {}).get("value")
    or 20
)

analysis_template_name = f"{service}-error-rate"
analysis_interval = str(strategy_analysis.get("interval") or "30s")
analysis_failure_limit = int(strategy_analysis.get("maxFailures") or 1)

metrics = []
for metric_id in metric_ids:
    if metric_id not in objectives:
        raise SystemExit(f"ERROR: strategy references missing SLO objective: {metric_id}")

    obj = objectives[metric_id]
    metrics.append({
        "name": metric_id,
        "interval": analysis_interval,
        "count": 3,
        "failureLimit": analysis_failure_limit,
        "successCondition": success_condition(metric_id, obj),
        "provider": {
            "prometheus": {
                "address": prometheus_addr,
                "query": prom_query(metric_id, obj, namespace, window, prometheus_metric_bindings),
            }
        },
    })

analysis_yaml = {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "AnalysisTemplate",
    "metadata": {
        "name": analysis_template_name,
        "namespace": namespace,
        "labels": {
            "app": service,
            "ssentinel.io/generated-by": "config-compiler",
            "ssentinel.io/env": env_name,
        },
        "annotations": renderer_annotations(analysis_template_renderer, "AnalysisTemplate"),
    },
    "spec": {
        "metrics": metrics,
    },
}

rollout_steps = []
traffic_steps = strategy_spec.get("traffic", {}).get("steps") or []
for step in traffic_steps:
    weight = step.get("setWeight")
    if weight is None:
        raise SystemExit(f"ERROR: traffic step missing setWeight: {step}")

    rollout_steps.append({"setWeight": int(weight)})

    pause = str(step.get("pause") or "0s")
    if pause and pause != "0s":
        rollout_steps.append({"pause": {"duration": pause}})

    if int(weight) < 100:
        rollout_steps.append({
            "analysis": {
                "templates": [
                    {"templateName": analysis_template_name}
                ]
            }
        })

rollout_yaml = {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Rollout",
    "metadata": {
        "name": service,
        "namespace": namespace,
        "labels": {
            "app": service,
            "ssentinel.io/generated-by": "config-compiler",
            "ssentinel.io/env": env_name,
        },
        "annotations": renderer_annotations(rollout_template_renderer, "Rollout"),
    },
    "spec": {
        "replicas": replicas,
        "revisionHistoryLimit": revision_history_limit,
        "selector": {
            "matchLabels": {
                "app": service,
            }
        },
        "template": {
            "metadata": {
                "labels": {
                    "app": service,
                    "version": image_tag,
                }
            },
            "spec": {
                "containers": [
                    {
                        "name": container_name,
                        "image": remote_image,
                        "imagePullPolicy": image_pull_policy,
                        "ports": [
                            {
                                "containerPort": container_port,
                                "name": service_port_name,
                            }
                        ],
                        "env": [
                            {"name": "VERSION", "value": app_version},
                            {"name": "RELEASE_TAG", "value": image_tag},
                            {"name": "FAULT_RATE", "value": fault_rate},
                            {"name": "LATENCY_MS", "value": latency_ms},
                        ],
                        "readinessProbe": {
                            "httpGet": {
                                "path": readiness_path,
                                "port": container_port,
                            },
                            "initialDelaySeconds": 3,
                            "periodSeconds": 5,
                        },
                        "livenessProbe": {
                            "httpGet": {
                                "path": liveness_path,
                                "port": container_port,
                            },
                            "initialDelaySeconds": 10,
                            "periodSeconds": 10,
                        },
                    }
                ]
            },
        },
        "strategy": {
            "canary": {
                "steps": rollout_steps,
            }
        },
    },
}


def pascal_case(value: str) -> str:
    parts = []
    current = []
    for ch in str(value):
        if ch.isalnum():
            current.append(ch)
        elif current:
            parts.append("".join(current))
            current = []
    if current:
        parts.append("".join(current))

    converted = []
    for part in parts:
        if part.lower().startswith("p") and part[1:].isdigit():
            converted.append(part.upper())
        elif part.isupper():
            converted.append(part)
        else:
            converted.append(part[:1].upper() + part[1:])

    return "".join(converted)


def alert_name_for(service_name: str, metric_id: str) -> str:
    return f"{pascal_case(service_name)}Canary{pascal_case(metric_id)}SLOViolation"


rules = []
for metric_id in metric_ids:
    obj = objectives[metric_id]
    expr = alert_expr(metric_id, obj, namespace, min_request_count, window, prometheus_metric_bindings)
    if expr is None:
        continue

    obj_type = str(obj.get("type", ""))
    _operator, value, unit = threshold(obj)

    if obj_type == "error_rate":
        alert_name = alert_name_for(service, metric_id)
        summary = f"{service} canary error rate is too high"
        description = f'version={{{{ $labels.version }}}} 5xx error rate is above {value}{unit if unit != "percent" else "%"}.'
    elif obj_type == "latency":
        percentile = obj.get("percentile", 95)
        alert_name = alert_name_for(service, metric_id)
        summary = f"{service} canary p{percentile} latency is too high"
        description = f'version={{{{ $labels.version }}}} p{percentile} latency is above {value}s.'
    else:
        continue

    rules.append({
        "alert": alert_name,
        "expr": expr,
        "for": "30s",
        "labels": {
            "severity": str(obj.get("severity") or "warning"),
            "project": project_name,
            "component": service,
            "alert_type": "rollout-slo",
            "ssentinel.io/generated-by": "config-compiler",
            "ssentinel.io/env": env_name,
        },
        "annotations": {
            "summary": summary,
            "description": description,
            "runbook": f"kubectl describe rollout {service} -n {namespace}",
        },
    })

prometheusrule_yaml = {
    "apiVersion": "monitoring.coreos.com/v1",
    "kind": "PrometheusRule",
    "metadata": {
        "name": f"{service}-rollout-alerts",
        "namespace": prometheus_rule_namespace,
        "labels": {
            "release": "prometheus-stack",
            "ssentinel.io/generated-by": "config-compiler",
            "ssentinel.io/env": env_name,
        },
        "annotations": renderer_annotations(prometheus_rule_renderer, "PrometheusRule"),
    },
    "spec": {
        "groups": [
            {
                "name": f"{service}-rollout.rules",
                "rules": rules,
            }
        ]
    },
}

kustomization_yaml = {
    "apiVersion": "kustomize.config.k8s.io/v1beta1",
    "kind": "Kustomization",
    "metadata": {
        "name": f"{service}-{env_name}-compiled",
        "annotations": renderer_annotations(environment_overlay_renderer, "Kustomization"),
    },
    "resources": [
        "analysis.yaml",
        "prometheusrule.yaml",
        "rollout.yaml",
    ],
}


source_config_refs = {
    "environmentConfig": {
        "path": env_ref,
        "kind": env_doc.get("kind"),
        "name": config_name(env_doc),
        "env": env_name,
    },
    "sloConfig": {
        "path": slo_ref,
        "kind": slo_doc.get("kind"),
        "name": config_name(slo_doc),
        "service": config_service(slo_doc),
    },
    "progressiveDeliveryStrategy": {
        "path": strategy_ref,
        "kind": strategy_doc.get("kind"),
        "name": config_name(strategy_doc),
        "service": config_service(strategy_doc),
    },
}

if compiler_profile_ref:
    source_config_refs["compilerProfile"] = {
        "path": compiler_profile_ref,
        "kind": compiler_profile_doc.get("kind"),
        "name": config_name(compiler_profile_doc),
        "service": config_service(compiler_profile_doc),
    }

hardcode_inventory = {
    "schemaVersion": "ssentinel.hardcode-inventory/v1alpha1",
    "status": "known_demo_bindings_present",
    "mode": "inventory_only",
    "summary": "RenderedReleasePlan records known demo bindings so they can be removed safely in later hardening steps.",
    "remainingBindings": [
        {
            "id": "demo-runtime-fault-env",
            "type": "demo-runtime-knob",
            "field": "Rollout.container.env",
            "value": "FAULT_RATE/LATENCY_MS",
            "resolution": "Move demo-only runtime knobs into a service profile or test fixture.",
        },
    ],
    "guardrails": {
        "inventoryOnly": True,
        "doesNotChangeRenderedManifests": True,
        "doesNotApplyKubernetes": True,
    },
}

rendered_release_plan = {
    "schemaVersion": "ssentinel.rendered-release-plan/v1alpha1",
    "kind": "RenderedReleasePlan",
    "generatedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    "generatedBy": "scripts/compile-release-config.sh",
    "release": {
        "service": service,
        "serviceSource": service_source,
        "env": env_name,
        "namespace": namespace,
        "clusterName": cluster_name,
        "environmentClass": environment_class,
        "policyProfile": policy_profile,
        "project": project_name,
        "imageRepository": image_repository,
        "image": remote_image,
        "imageTag": image_tag,
        "appVersion": app_version,
        "faultRate": fault_rate,
        "latencyMs": latency_ms,
    },
    "inputs": {
        "environmentConfigRef": env_ref,
        "sloConfigRef": slo_ref,
        "strategyConfigRef": strategy_ref,
        "compilerProfileRef": compiler_profile_ref,
        "overlayPath": overlay_path,
    },
    "sourceConfigRefs": source_config_refs,
    "compilerProfile": {
        "enabled": bool(compiler_profile_doc),
        "profileId": config_name(compiler_profile_doc) if compiler_profile_doc else None,
        "profileRef": compiler_profile_ref,
        "apiVersion": compiler_profile_doc.get("apiVersion"),
        "kind": compiler_profile_doc.get("kind"),
        "serviceConfig": compiler_profile_spec.get("serviceConfig") or {},
        "runtimeProfile": compiler_profile_spec.get("runtimeProfile") or {},
        "metricBinding": compiler_profile_spec.get("metricBinding") or {},
        "rendererRefs": compiler_profile_spec.get("rendererRefs") or {},
        "guardrails": compiler_profile_guardrails,
    },
    "hardcodeInventory": hardcode_inventory,
    "slo": {
        "sloId": config_name(slo_doc),
        "window": window,
        "minRequestCount": min_request_count,
        "observability": {
            "prometheus": prometheus_metric_bindings,
        },
        "objectives": [
            {
                "id": metric_id,
                "type": objectives[metric_id].get("type"),
                "threshold": objectives[metric_id].get("threshold"),
                "severity": objectives[metric_id].get("severity"),
            }
            for metric_id in metric_ids
        ],
    },
    "strategy": {
        "strategyId": config_name(strategy_doc),
        "strategyType": strategy_spec.get("strategyType"),
        "trafficSteps": traffic_steps,
        "analysis": strategy_analysis,
        "failurePolicy": strategy_spec.get("failurePolicy") or {},
        "promotionPolicy": strategy_spec.get("promotionPolicy") or {},
    },
    "outputs": {
        "outputDir": str(out_dir.relative_to(root) if out_dir.is_relative_to(root) else out_dir),
        "analysisTemplate": "analysis.yaml",
        "rollout": "rollout.yaml",
        "prometheusRule": "prometheusrule.yaml",
        "kustomization": "kustomization.yaml",
        "renderedReleasePlan": "rendered-release-plan.json",
        "rendererRefs": renderer_contracts,
        "artifacts": [
            {
                "kind": "AnalysisTemplate",
                "path": "analysis.yaml",
                "rendererRef": analysis_template_renderer,
            },
            {
                "kind": "Rollout",
                "path": "rollout.yaml",
                "rendererRef": rollout_template_renderer,
            },
            {
                "kind": "PrometheusRule",
                "path": "prometheusrule.yaml",
                "rendererRef": prometheus_rule_renderer,
            },
            {
                "kind": "Kustomization",
                "path": "kustomization.yaml",
                "rendererRef": environment_overlay_renderer,
            },
        ],
    },
    "guardrails": {
        "doesNotApplyKubernetes": True,
        "doesNotCommitOrPush": True,
        "doesNotBuildImages": True,
        "doesNotModifyCluster": True,
    },
}

out_dir.mkdir(parents=True, exist_ok=True)

write_yaml(out_dir / "analysis.yaml", analysis_yaml)
write_yaml(out_dir / "rollout.yaml", rollout_yaml)
write_yaml(out_dir / "prometheusrule.yaml", prometheusrule_yaml)
write_yaml(out_dir / "kustomization.yaml", kustomization_yaml)
write_json(out_dir / "rendered-release-plan.json", rendered_release_plan)

print(f"PASS: compiled release config env={env_name} service={service}")
print(f"outputDir={out_dir}")
print(f"analysisTemplate={out_dir / 'analysis.yaml'}")
print(f"rollout={out_dir / 'rollout.yaml'}")
print(f"prometheusRule={out_dir / 'prometheusrule.yaml'}")
print(f"renderedReleasePlan={out_dir / 'rendered-release-plan.json'}")
PY
