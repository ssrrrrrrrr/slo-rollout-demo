package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type OperationInspectionStatus string

const (
	OperationInspectionApplied    OperationInspectionStatus = "APPLIED"
	OperationInspectionNotApplied OperationInspectionStatus = "NOT_APPLIED"
	OperationInspectionUnknown    OperationInspectionStatus = "UNKNOWN"
)

// OperationExecutionInspector observes external state only. It has no
// mutation surface and is intentionally separate from post-action Verify.
type OperationExecutionInspector interface {
	Inspect(context.Context, ControlledOperation) (OperationInspectionResult, error)
}

type OperationInspectionResult struct {
	Status            OperationInspectionStatus
	Reason            string
	ExternalReference string
}

type OperationExecutionInspectorRegistry struct{ inspectors []OperationExecutionInspector }

func NewOperationExecutionInspectorRegistry(inspectors ...OperationExecutionInspector) *OperationExecutionInspectorRegistry {
	return &OperationExecutionInspectorRegistry{inspectors: inspectors}
}

func (r *OperationExecutionInspectorRegistry) Inspect(ctx context.Context, operation ControlledOperation) (OperationInspectionResult, error) {
	for _, inspector := range r.inspectors {
		result, err := inspector.Inspect(ctx, operation)
		if err == nil && result.Status != "" {
			return result, nil
		}
		if err != nil {
			return OperationInspectionResult{}, err
		}
	}
	return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "no execution inspector supports this operation"}, nil
}

// KubernetesOperationExecutionInspector reads the configured Rollout state
// without issuing an Update. Restart and scale checks use persisted intent,
// not a newly calculated value.
type KubernetesOperationExecutionInspector struct {
	client  dynamic.Interface
	initErr error
}

func NewKubernetesOperationExecutionInspector(client dynamic.Interface) *KubernetesOperationExecutionInspector {
	return &KubernetesOperationExecutionInspector{client: client}
}

func (i *KubernetesOperationExecutionInspector) Inspect(ctx context.Context, operation ControlledOperation) (OperationInspectionResult, error) {
	if operation.Action != OperationRestartWorkload && operation.Action != OperationScaleWorkload {
		return OperationInspectionResult{}, nil
	}
	if i.initErr != nil {
		return OperationInspectionResult{}, i.initErr
	}
	if i.client == nil {
		return OperationInspectionResult{}, fmt.Errorf("Kubernetes operation inspector is unavailable")
	}
	target := operation.ExecutionIntent.Target
	if target.Namespace == "" || target.WorkloadKind != "Rollout" || target.WorkloadName == "" {
		return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "operation execution intent has no configured Rollout target"}, nil
	}
	rollout, err := i.client.Resource(rolloutGVR).Namespace(target.Namespace).Get(ctx, target.WorkloadName, metav1.GetOptions{})
	if err != nil {
		return OperationInspectionResult{}, err
	}
	switch operation.Action {
	case OperationRestartWorkload:
		if operation.ExecutionIntent.RestartAt == "" {
			return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "restart execution intent has no fixed restartAt"}, nil
		}
		observed, _, _ := unstructured.NestedString(rollout.Object, "spec", "restartAt")
		if observed == operation.ExecutionIntent.RestartAt {
			return OperationInspectionResult{Status: OperationInspectionApplied, Reason: "Rollout spec.restartAt matches the persisted restart intent", ExternalReference: target.Namespace + "/" + target.WorkloadName}, nil
		}
		return OperationInspectionResult{Status: OperationInspectionNotApplied, Reason: "Rollout spec.restartAt does not match the persisted restart intent", ExternalReference: target.Namespace + "/" + target.WorkloadName}, nil
	case OperationScaleWorkload:
		if operation.ExecutionIntent.TargetReplicas == nil {
			return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "scale execution intent has no fixed targetReplicas"}, nil
		}
		if rolloutReplicas(rollout) == *operation.ExecutionIntent.TargetReplicas {
			return OperationInspectionResult{Status: OperationInspectionApplied, Reason: "Rollout spec.replicas matches the persisted scale target", ExternalReference: target.Namespace + "/" + target.WorkloadName}, nil
		}
		return OperationInspectionResult{Status: OperationInspectionNotApplied, Reason: "Rollout spec.replicas does not match the persisted scale target", ExternalReference: target.Namespace + "/" + target.WorkloadName}, nil
	}
	return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "unsupported Kubernetes operation"}, nil
}

// RuntimeActionExecutionResultInspector only reads the existing Runtime Action
// result artifact. It never calls the execution script and never mutates a
// release while inspecting crash recovery.
type RuntimeActionExecutionResultInspector struct{ reportDir string }

func NewRuntimeActionExecutionResultInspector(reportDir string) *RuntimeActionExecutionResultInspector {
	return &RuntimeActionExecutionResultInspector{reportDir: reportDir}
}

func (i *RuntimeActionExecutionResultInspector) Inspect(_ context.Context, operation ControlledOperation) (OperationInspectionResult, error) {
	if operation.Action != OperationPauseRelease && operation.Action != OperationResumeRelease && operation.Action != OperationPromoteRelease && operation.Action != OperationAbortRelease && operation.Action != OperationRollbackRelease {
		return OperationInspectionResult{}, nil
	}
	releaseID := operation.ExecutionIntent.ReleaseID
	if releaseID == "" {
		return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "release execution intent has no release ID"}, nil
	}
	path := filepath.Join(i.reportDir, "runtime-action-execution-result-"+releaseID+".json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "existing runtime action execution result is unavailable"}, nil
		}
		return OperationInspectionResult{}, err
	}
	document, err := loadRemediationJSON(path)
	if err != nil {
		return OperationInspectionResult{}, err
	}
	if nestedString(document, "release", "releaseId") != releaseID || nestedString(document, "action", "requestedAction") != runtimeActionName(operationRemediationAction(operation.Action)) {
		return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "runtime action execution result does not match the persisted release intent"}, nil
	}
	resultID := stringValue(document["runtimeActionExecutionResultId"])
	if nestedString(document, "result", "executionStatus") == "SUCCEEDED" && nestedBool(document, "verificationSummary", "verified") && nestedString(document, "postActionVerification", "verificationStatus") == "VERIFIED" {
		return OperationInspectionResult{Status: OperationInspectionApplied, Reason: "existing runtime action execution result is verified", ExternalReference: resultID}, nil
	}
	if status := nestedString(document, "result", "executionStatus"); status == "FAILED" || status == "BLOCKED" {
		return OperationInspectionResult{Status: OperationInspectionNotApplied, Reason: "existing runtime action execution result reports " + status, ExternalReference: resultID}, nil
	}
	return OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "existing runtime action execution result is inconclusive", ExternalReference: resultID}, nil
}

func operationRemediationAction(action OperationAction) string {
	switch action {
	case OperationPauseRelease:
		return "PAUSE"
	case OperationResumeRelease:
		return "RESUME"
	case OperationPromoteRelease:
		return "PROMOTE"
	case OperationAbortRelease:
		return "ABORT"
	case OperationRollbackRelease:
		return "ROLLBACK"
	}
	return ""
}
