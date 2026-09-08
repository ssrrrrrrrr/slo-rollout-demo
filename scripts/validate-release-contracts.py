#!/usr/bin/env python3
import argparse
import json
import sys
from pathlib import Path
from typing import Any


class ValidationError(Exception):
    pass


def load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8-sig") as f:
        return json.load(f)


def json_type(value: Any) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, int) and not isinstance(value, bool):
        return "integer"
    if isinstance(value, float):
        return "number"
    if isinstance(value, str):
        return "string"
    if isinstance(value, list):
        return "array"
    if isinstance(value, dict):
        return "object"
    return type(value).__name__


def matches_type(value: Any, expected: Any) -> bool:
    actual = json_type(value)

    if isinstance(expected, list):
        return any(matches_type(value, item) for item in expected)

    if expected == "number":
        return actual in ("integer", "number")

    return actual == expected


def validate_node(value: Any, schema: dict[str, Any], path: str) -> list[str]:
    errors: list[str] = []

    if "type" in schema and not matches_type(value, schema["type"]):
        errors.append(f"{path}: expected type {schema['type']}, got {json_type(value)}")
        return errors

    if "const" in schema and value != schema["const"]:
        errors.append(f"{path}: expected const {schema['const']!r}, got {value!r}")

    if "enum" in schema and value not in schema["enum"]:
        errors.append(f"{path}: expected one of {schema['enum']}, got {value!r}")

    if isinstance(value, str):
        if "minLength" in schema and len(value) < schema["minLength"]:
            errors.append(f"{path}: expected minLength {schema['minLength']}, got {len(value)}")

    if isinstance(value, (int, float)) and not isinstance(value, bool):
        if "minimum" in schema and value < schema["minimum"]:
            errors.append(f"{path}: expected minimum {schema['minimum']}, got {value}")
        if "maximum" in schema and value > schema["maximum"]:
            errors.append(f"{path}: expected maximum {schema['maximum']}, got {value}")

    if isinstance(value, list):
        item_schema = schema.get("items")
        if isinstance(item_schema, dict):
            for idx, item in enumerate(value):
                errors.extend(validate_node(item, item_schema, f"{path}[{idx}]"))

    if isinstance(value, dict):
        required = schema.get("required", [])
        for key in required:
            if key not in value:
                errors.append(f"{path}: missing required property {key!r}")

        properties = schema.get("properties", {})
        for key, prop_schema in properties.items():
            if key in value and isinstance(prop_schema, dict):
                errors.extend(validate_node(value[key], prop_schema, f"{path}.{key}"))

        additional = schema.get("additionalProperties", True)
        if additional is False:
            allowed = set(properties.keys())
            for key in value:
                if key not in allowed:
                    errors.append(f"{path}: unexpected property {key!r}")
        elif isinstance(additional, dict):
            for key, child in value.items():
                if key not in properties:
                    errors.extend(validate_node(child, additional, f"{path}.{key}"))

    return errors


def infer_schema_name(document: Any, file_name: str) -> str:
    if isinstance(document, dict):
        schema_version = document.get("schemaVersion")
        if schema_version == "release.evidence.bundle/v1alpha1":
            return "release-evidence.schema.json"
        if schema_version == "release.policy.evaluator/v1alpha1":
            return "policy-decision.schema.json"
        if schema_version == "release.timeline/v1alpha1":
            return "release-timeline.schema.json"
        if schema_version == "evidence.record/v1alpha1":
            return "evidence-record.schema.json"
        if schema_version == "agent.run/v1alpha1":
            return "agent-run.schema.json"
        if schema_version == "agent.plan.run/v1alpha1":
            return "plan-run.schema.json"
        if schema_version == "execution.request/v1alpha1":
            return "execution-request.schema.json"
        if schema_version == "supply.chain.decision/v1alpha1":
            return "supply-chain-decision.schema.json"

    lower_name = file_name.lower()
    if "release-context" in lower_name or lower_name.endswith("context-sample.json"):
        return "release-context.schema.json"
    if "policy-decision" in lower_name:
        return "policy-decision.schema.json"
    if "release-evidence" in lower_name or "evidence-sample" in lower_name:
        return "release-evidence.schema.json"
    if "ai-decision" in lower_name:
        return "ai-decision.schema.json"
    if "supply-chain-decision" in lower_name:
        return "supply-chain-decision.schema.json"
    if "execution-request" in lower_name:
        return "execution-request.schema.json"
    if "plan-run" in lower_name:
        return "plan-run.schema.json"
    if "action-plan" in lower_name:
        return "action-plan.schema.json"
    if "release-intelligence" in lower_name or "intelligence-sample" in lower_name:
        return "release-intelligence.schema.json"
    if "release-timeline" in lower_name or "timeline-sample" in lower_name:
        return "release-timeline.schema.json"
    if "evidence-record" in lower_name:
        return "evidence-record.schema.json"
    if "agent-run" in lower_name:
        return "agent-run.schema.json"

    raise ValidationError(
        f"cannot infer schema for {file_name}; use a file name containing release-context, policy-decision, release-evidence, ai-decision, action-plan, plan-run, execution-request, supply-chain-decision, release-intelligence, release-timeline, evidence-record, agent-run"
    )


def validate_file(schema_dir: Path, json_file: Path) -> tuple[bool, list[str]]:
    document = load_json(json_file)
    schema_name = infer_schema_name(document, json_file.name)
    schema_path = schema_dir / schema_name

    if not schema_path.exists():
        raise ValidationError(f"schema not found: {schema_path}")

    schema = load_json(schema_path)
    errors = validate_node(document, schema, "$")
    return len(errors) == 0, errors


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate S Sentinel release evidence contract JSON files."
    )
    parser.add_argument(
        "--schema-dir",
        default="schemas",
        help="Directory containing release contract schema JSON files.",
    )
    parser.add_argument(
        "json_files",
        nargs="+",
        help="JSON files to validate.",
    )
    args = parser.parse_args()

    schema_dir = Path(args.schema_dir)
    failed = False

    for item in args.json_files:
        json_file = Path(item)
        try:
            ok, errors = validate_file(schema_dir, json_file)
        except Exception as exc:
            failed = True
            print(f"FAIL {json_file}: {exc}")
            continue

        if ok:
            print(f"PASS {json_file}")
        else:
            failed = True
            print(f"FAIL {json_file}")
            for error in errors:
                print(f"  - {error}")

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
