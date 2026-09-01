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
	resource := e.client.Resource(rolloutGVR).Namespace(p.Target.Namespace)
	rollout, err := resource.Get(ctx, p.Target.Name, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	switch p.Action.Type {
	case RecoveryRestartWorkload:
		if err := unstructured.SetNestedField(rollout.Object, time.Now().UTC().Format(time.RFC3339), "spec", "restartAt"); err != nil {
			return 0, err
		}
		_, err = resource.Update(ctx, rollout, metav1.UpdateOptions{})
		return rolloutReplicas(rollout), err
	case RecoveryScaleWorkload:
		current := rolloutReplicas(rollout)
		params := p.Action.Parameters
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
		if target == current {
			return target, fmt.Errorf("scale target is already within its configured bound")
		}
		if err := unstructured.SetNestedField(rollout.Object, target, "spec", "replicas"); err != nil {
			return 0, err
		}
		_, err = resource.Update(ctx, rollout, metav1.UpdateOptions{})
		return target, err
	}
	return 0, fmt.Errorf("unsupported recovery action")
}
func rolloutReplicas(o *unstructured.Unstructured) int64 { return getInt64(o, "spec", "replicas") }
