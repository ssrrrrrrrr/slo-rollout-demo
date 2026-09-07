package main

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"time"
)

// KubernetesRecoveryExecutor accepts only plan-derived Rollout operations; it
// has no shell, pod-delete, or client-supplied target surface.
type KubernetesRecoveryExecutor struct {
	client  dynamic.Interface
	initErr error
}

func NewKubernetesRecoveryExecutor() *KubernetesRecoveryExecutor {
	cfg, err := buildKubeConfig()
	if err != nil {
		return &KubernetesRecoveryExecutor{initErr: err}
	}
	c, err := dynamic.NewForConfig(cfg)
	return &KubernetesRecoveryExecutor{client: c, initErr: err}
}
func NewKubernetesRecoveryExecutorForClient(client dynamic.Interface) *KubernetesRecoveryExecutor {
	return &KubernetesRecoveryExecutor{client: client}
}
func (e *KubernetesRecoveryExecutor) Supports(a RecoveryActionType) bool {
	return a == RecoveryRestartWorkload || a == RecoveryScaleWorkload
}
func (e *KubernetesRecoveryExecutor) Preflight(_ context.Context, p RecoveryPlan) error {
	if e.initErr != nil {
		return e.initErr
	}
	if e.client == nil {
		return fmt.Errorf("Kubernetes recovery client is unavailable")
	}
	if p.Target.Kind != "Rollout" || p.Target.Namespace == "" || p.Target.Name == "" {
		return fmt.Errorf("recovery target is not a configured Rollout")
	}
	return nil
}
func (e *KubernetesRecoveryExecutor) Execute(ctx context.Context, p RecoveryPlan) (int64, error) {
	if err := e.Preflight(ctx, p); err != nil {
		return 0, err
	}
	return e.ExecutePreflighted(ctx, p)
}

// ExecutePreflighted is intentionally internal to the controlled operation
// adapter. It preserves the established mutation algorithm while allowing the
// adapter to avoid evaluating the same preflight twice.
func (e *KubernetesRecoveryExecutor) ExecutePreflighted(ctx context.Context, p RecoveryPlan) (int64, error) {
	intent := OperationExecutionIntent{Action: recoveryOperationAction(p.Action.Type), Target: OperationTarget{Namespace: p.Target.Namespace, WorkloadKind: p.Target.Kind, WorkloadName: p.Target.Name}}
	if p.Action.Type == RecoveryRestartWorkload {
		intent.RestartAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if p.Action.Type == RecoveryScaleWorkload {
		rollout, err := e.client.Resource(rolloutGVR).Namespace(p.Target.Namespace).Get(ctx, p.Target.Name, metav1.GetOptions{})
		if err != nil {
			return 0, err
		}
		current := rolloutReplicas(rollout)
		target := boundedScaleTarget(current, p.Action.Parameters)
		if target == current {
			return target, fmt.Errorf("scale target is already within its configured bound")
		}
		intent.InitialReplicas, intent.TargetReplicas = &current, &target
	}
	return e.executeIntent(ctx, intent)
}

// ExecuteOperation consumes the immutable Operation intent written before
// EXECUTING. It is the production ledger path and never regenerates restartAt
// or a scale target.
func (e *KubernetesRecoveryExecutor) ExecuteOperation(ctx context.Context, op ControlledOperation) (int64, error) {
	p := RecoveryPlan{Action: RunbookAction{Type: RecoveryRestartWorkload}, Target: RecoveryTarget{Namespace: op.ExecutionIntent.Target.Namespace, Kind: op.ExecutionIntent.Target.WorkloadKind, Name: op.ExecutionIntent.Target.WorkloadName}}
	if op.Action == OperationScaleWorkload {
		p.Action.Type = RecoveryScaleWorkload
	}
	if err := e.Preflight(ctx, p); err != nil {
		return 0, err
	}
	return e.executeIntent(ctx, op.ExecutionIntent)
}

func (e *KubernetesRecoveryExecutor) executeIntent(ctx context.Context, intent OperationExecutionIntent) (int64, error) {
	target := intent.Target
	if target.Namespace == "" || target.WorkloadKind != "Rollout" || target.WorkloadName == "" {
		return 0, fmt.Errorf("operation execution intent has no configured Rollout target")
	}
	resource := e.client.Resource(rolloutGVR).Namespace(target.Namespace)
	rollout, err := resource.Get(ctx, target.WorkloadName, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	switch intent.Action {
	case OperationRestartWorkload:
		if intent.RestartAt == "" {
			return 0, fmt.Errorf("restart execution intent has no fixed restartAt")
		}
		if err := unstructured.SetNestedField(rollout.Object, intent.RestartAt, "spec", "restartAt"); err != nil {
			return 0, err
		}
		_, err = resource.Update(ctx, rollout, metav1.UpdateOptions{})
		return rolloutReplicas(rollout), err
	case OperationScaleWorkload:
		if intent.TargetReplicas == nil {
			return 0, fmt.Errorf("scale execution intent has no fixed targetReplicas")
		}
		if err := unstructured.SetNestedField(rollout.Object, *intent.TargetReplicas, "spec", "replicas"); err != nil {
			return 0, err
		}
		_, err = resource.Update(ctx, rollout, metav1.UpdateOptions{})
		return *intent.TargetReplicas, err
	}
	return 0, fmt.Errorf("unsupported recovery action")
}

func boundedScaleTarget(current int64, params RecoveryActionParameters) int64 {
	target := current + params.Step
	if params.Direction == "DOWN" {
		target = current - params.Step
	}
	if target < params.MinReplicas {
		target = params.MinReplicas
	}
	if target > params.MaxReplicas {
		target = params.MaxReplicas
	}
	return target
}
func rolloutReplicas(o *unstructured.Unstructured) int64 { return getInt64(o, "spec", "replicas") }
