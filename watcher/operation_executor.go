package main

import (
	"context"
	"fmt"
	"time"
)

type OperationExecutionResult struct {
	Execution        OperationExecutionState
	ExternalTarget   OperationTarget
	ExpectedReplicas int64
	PostState        map[string]interface{}
	ActionVerified   bool
}

type OperationExecutor interface {
	Supports(OperationAction) bool
	Execute(context.Context, ControlledOperation) (OperationExecutionResult, error)
}

type OperationExecutorRegistry struct{ executors []OperationExecutor }

func NewOperationExecutorRegistry(executors ...OperationExecutor) *OperationExecutorRegistry {
	return &OperationExecutorRegistry{executors: executors}
}

func (r *OperationExecutorRegistry) Execute(ctx context.Context, op ControlledOperation) (OperationExecutionResult, error) {
	for _, executor := range r.executors {
		if executor.Supports(op.Action) {
			return executor.Execute(ctx, op)
		}
	}
	return OperationExecutionResult{}, fmt.Errorf("no operation executor supports %s", op.Action)
}

// ReleaseRuntimeActionExecutorAdapter delegates to the existing Runtime Action
// pipeline. It introduces neither commands nor another release executor.
type ReleaseRuntimeActionExecutorAdapter struct{ adapter RemediationExecutionAdapter }

func (a ReleaseRuntimeActionExecutorAdapter) Supports(action OperationAction) bool {
	switch action {
	case OperationPauseRelease, OperationResumeRelease, OperationPromoteRelease, OperationAbortRelease, OperationRollbackRelease:
		return true
	}
	return false
}

func (a ReleaseRuntimeActionExecutorAdapter) Execute(ctx context.Context, op ControlledOperation) (OperationExecutionResult, error) {
	if a.adapter == nil {
		return OperationExecutionResult{}, fmt.Errorf("release runtime action adapter unavailable")
	}
	action := map[OperationAction]string{
		OperationPauseRelease: "PAUSE", OperationResumeRelease: "RESUME", OperationPromoteRelease: "PROMOTE",
		OperationAbortRelease: "ABORT", OperationRollbackRelease: "ROLLBACK",
	}[op.Action]
	result, err := a.adapter.Execute(ctx, RemediationExecutionRequest{ReleaseID: op.Target.ReleaseID, Action: action})
	state := OperationExecutionState{Status: result.Status, Executor: "release-runtime-action", StartedAt: result.StartedAt, FinishedAt: result.FinishedAt, Reason: result.Reason, ExternalResultID: result.ResultID}
	if state.Status == "" {
		state.Status = "FAILED"
	}
	return OperationExecutionResult{
		Execution:      state,
		ExternalTarget: OperationTarget{ReleaseID: op.Target.ReleaseID, Namespace: result.Target.Namespace, WorkloadName: result.Target.Workload},
		PostState:      result.PostState, ActionVerified: result.ActionVerified,
	}, err
}

// KubernetesRecoveryExecutorAdapter is a thin adapter over the established
// bounded RecoveryExecutor. It calls the existing preflight exactly once.
type KubernetesRecoveryExecutorAdapter struct{ executor RecoveryExecutor }

func (a KubernetesRecoveryExecutorAdapter) Supports(action OperationAction) bool {
	return action == OperationRestartWorkload || action == OperationScaleWorkload
}

func (a KubernetesRecoveryExecutorAdapter) Execute(ctx context.Context, op ControlledOperation) (OperationExecutionResult, error) {
	if a.executor == nil {
		return OperationExecutionResult{}, fmt.Errorf("Kubernetes recovery executor unavailable")
	}
	action := RecoveryRestartWorkload
	if op.Action == OperationScaleWorkload {
		action = RecoveryScaleWorkload
	}
	plan := RecoveryPlan{Action: RunbookAction{Type: action, Parameters: op.Parameters}, Target: RecoveryTarget{Namespace: op.Target.Namespace, Kind: op.Target.WorkloadKind, Name: op.Target.WorkloadName}}
	if err := a.executor.Preflight(ctx, plan); err != nil {
		return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "kubernetes-recovery", Reason: err.Error()}}, err
	}
	started := time.Now().UTC().Format(time.RFC3339)
	var replicas int64
	var err error
	if executor, ok := a.executor.(interface {
		ExecutePreflighted(context.Context, RecoveryPlan) (int64, error)
	}); ok {
		replicas, err = executor.ExecutePreflighted(ctx, plan)
	} else {
		replicas, err = a.executor.Execute(ctx, plan)
	}
	state := OperationExecutionState{Status: "SUCCEEDED", Executor: "kubernetes-recovery", StartedAt: started, FinishedAt: time.Now().UTC().Format(time.RFC3339)}
	if err != nil {
		state.Status, state.Reason = "FAILED", err.Error()
	}
	return OperationExecutionResult{Execution: state, ExpectedReplicas: replicas}, err
}
