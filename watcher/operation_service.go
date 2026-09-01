package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// OperationService owns the process-local execution idempotency map. It is
// intentionally not durable: an operation may be re-evaluated after watcher
// restart, matching the existing Remediation and Recovery behavior.
type OperationService struct {
	registry   *OperationExecutorRegistry
	lifecycle  *IncidentLifecycleService
	mu         sync.Mutex
	executions map[string]OperationExecutionResult
}

func NewOperationService(registry *OperationExecutorRegistry) *OperationService {
	return &OperationService{registry: registry, executions: map[string]OperationExecutionResult{}}
}

func (api *portalAPI) operationService() *OperationService {
	if api.operationSvc == nil {
		api.operationSvc = NewOperationService(NewOperationExecutorRegistry(
			ReleaseRuntimeActionExecutorAdapter{adapter: NewRuntimeActionPipelineAdapter(api.cfg.RepoDir, api.reportDir)},
			KubernetesRecoveryExecutorAdapter{executor: NewKubernetesRecoveryExecutor()},
		))
	}
	return api.operationSvc
}

func (s *OperationService) Execute(ctx context.Context, op ControlledOperation) (OperationExecutionResult, error) {
	if op.ID == "" {
		return OperationExecutionResult{}, fmt.Errorf("controlled operation ID is required")
	}
	if op.Preflight.Status != "READY" {
		reason := firstOperationReason(op.Preflight.BlockedReasons, "operation preflight is not ready")
		if s.lifecycle != nil {
			s.lifecycle.OperationBlocked(ctx, op, reason)
		}
		return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "operation-plane", Reason: reason}}, nil
	}
	if op.Policy.Decision == "DENY" || (op.Approval.Required && !op.Approval.Approved) {
		reason := op.Policy.Reason
		if reason == "" {
			reason = "operation policy or approval is not satisfied"
		}
		if s.lifecycle != nil {
			s.lifecycle.OperationBlocked(ctx, op, reason)
		}
		return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "operation-plane", Reason: reason}}, nil
	}
	s.mu.Lock()
	if result, ok := s.executions[op.IdempotencyKey]; ok {
		s.mu.Unlock()
		return result, nil
	}
	s.executions[op.IdempotencyKey] = OperationExecutionResult{Execution: OperationExecutionState{Status: "EXECUTING", Executor: "operation-plane", StartedAt: time.Now().UTC().Format(time.RFC3339)}}
	s.mu.Unlock()
	if s.lifecycle != nil {
		s.lifecycle.OperationStarted(ctx, op)
	}
	result, err := s.registry.Execute(ctx, op)
	s.mu.Lock()
	s.executions[op.IdempotencyKey] = result
	s.mu.Unlock()
	if s.lifecycle != nil {
		s.lifecycle.OperationCompleted(ctx, op, result, err)
	}
	return result, err
}

func firstOperationReason(reasons []string, fallback string) string {
	if len(reasons) > 0 && reasons[0] != "" {
		return reasons[0]
	}
	return fallback
}
