package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// OperationService executes only operations whose durable ledger has recorded
// immutable intent and EXECUTING. The repository, rather than a process-local
// map, is the authoritative idempotency source.
type OperationService struct {
	registry          *OperationExecutorRegistry
	incidentLifecycle *IncidentLifecycleService
	lifecycle         *OperationLifecycleService
	ledgerErr         error
	operationLocksMu  sync.Mutex
	operationLocks    map[string]chan struct{}
}

// OperationLedgerError marks a failure of the durable safety prerequisite.
// Callers surface it as BLOCKED and must not fall back to an in-memory path.
type OperationLedgerError struct{ Cause error }

func (e *OperationLedgerError) Error() string {
	if e == nil || e.Cause == nil {
		return "durable operation ledger is unavailable"
	}
	return e.Cause.Error()
}

func operationLedgerError(err error) error {
	if err == nil {
		return nil
	}
	return &OperationLedgerError{Cause: err}
}

func NewOperationService(registry *OperationExecutorRegistry) *OperationService {
	// Standalone domain construction remains useful for focused tests and is
	// never used by the portal's real execution path, which injects its
	// configured durable SQLite repository below.
	repo, err := NewSQLiteOperationRepository(":memory:")
	if err != nil {
		return &OperationService{registry: registry, ledgerErr: fmt.Errorf("durable operation ledger is unavailable: %w", err)}
	}
	return &OperationService{registry: registry, lifecycle: NewOperationLifecycleService(repo, nil), operationLocks: map[string]chan struct{}{}}
}

func NewDurableOperationService(registry *OperationExecutorRegistry, lifecycle *OperationLifecycleService) *OperationService {
	if lifecycle == nil {
		return NewOperationService(registry)
	}
	return &OperationService{registry: registry, lifecycle: lifecycle, operationLocks: map[string]chan struct{}{}}
}

func (api *portalAPI) operationService() *OperationService {
	if api.operationSvc == nil {
		kubernetes := NewKubernetesRecoveryExecutor()
		registry := NewOperationExecutorRegistry(
			ReleaseRuntimeActionExecutorAdapter{adapter: NewRuntimeActionPipelineAdapter(api.cfg.RepoDir, api.reportDir)},
			KubernetesRecoveryExecutorAdapter{executor: kubernetes},
		)
		repo, err := NewSQLiteOperationRepository(api.cfg.OperationStoreDB)
		if err != nil {
			log.Printf("durable operation ledger unavailable; controlled mutation is fail-closed: %v", err)
			service := NewOperationService(registry)
			service.ledgerErr = fmt.Errorf("durable operation ledger is unavailable: %w", err)
			api.operationSvc = service
			return api.operationSvc
		}
		inspector := NewOperationExecutionInspectorRegistry(
			&KubernetesOperationExecutionInspector{client: kubernetes.client, initErr: kubernetes.initErr},
			NewRuntimeActionExecutionResultInspector(api.reportDir),
		)
		api.operationSvc = NewDurableOperationService(registry, NewOperationLifecycleService(repo, inspector))
	}
	return api.operationSvc
}

func (s *OperationService) Execute(ctx context.Context, op ControlledOperation) (OperationExecutionResult, error) {
	if s.ledgerErr != nil || s.lifecycle == nil {
		err := operationLedgerError(fmt.Errorf("%s", firstNonEmpty(errorText(s.ledgerErr), "durable operation ledger is unavailable")))
		return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "operation-plane", Reason: err.Error()}}, err
	}
	if op.ID == "" {
		return OperationExecutionResult{}, fmt.Errorf("controlled operation ID is required")
	}
	release, err := s.lockOperation(ctx, op.ID)
	if err != nil {
		return OperationExecutionResult{}, err
	}
	defer release()
	durable, err := s.lifecycle.Create(ctx, op)
	if err != nil {
		return OperationExecutionResult{}, operationLedgerError(err)
	}
	if durable.State != OperationStatePlanned && durable.State != OperationStateWaitingApproval && durable.State != OperationStateReady {
		return s.reconcileExisting(ctx, *durable)
	}
	durable, err = s.lifecycle.RefreshConditions(ctx, durable.ID, op)
	if err != nil {
		return OperationExecutionResult{}, operationLedgerError(err)
	}
	if durable.Approval.Required && !durable.Approval.Approved {
		if durable.State == OperationStatePlanned {
			_, err = s.lifecycle.Transition(ctx, durable.ID, OperationStateWaitingApproval, "APPROVAL_REQUIRED", "Operation is waiting for durable approval", nil)
		}
		return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "operation-plane", Reason: "human approval required"}}, err
	}
	if durable.Preflight.Status != "READY" || durable.Policy.Decision == "DENY" {
		reason := firstOperationReason(durable.Preflight.BlockedReasons, firstNonEmpty(durable.Policy.Reason, "operation preflight is not ready"))
		if strings.Contains(strings.ToLower(reason), "approval") && durable.State == OperationStatePlanned {
			_, err = s.lifecycle.Transition(ctx, durable.ID, OperationStateWaitingApproval, "APPROVAL_REQUIRED", "Operation is waiting for durable approval", nil)
			return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "operation-plane", Reason: reason}}, err
		}
		if durable.State == OperationStatePlanned || durable.State == OperationStateWaitingApproval {
			_, transitionErr := s.lifecycle.Transition(ctx, durable.ID, OperationStateBlocked, "READY", reason, nil)
			if transitionErr != nil {
				return OperationExecutionResult{}, operationLedgerError(transitionErr)
			}
		}
		return OperationExecutionResult{Execution: OperationExecutionState{Status: "BLOCKED", Executor: "operation-plane", Reason: reason}}, nil
	}
	if durable.State == OperationStatePlanned || durable.State == OperationStateWaitingApproval {
		if durable, err = s.lifecycle.Transition(ctx, durable.ID, OperationStateReady, "READY", "Operation is ready for execution", nil); err != nil {
			return OperationExecutionResult{}, operationLedgerError(err)
		}
	}
	if durable, err = s.lifecycle.Transition(ctx, durable.ID, OperationStateExecuting, "EXECUTION_STARTED", "Operation execution started", nil); err != nil {
		return OperationExecutionResult{}, operationLedgerError(err)
	}
	if s.incidentLifecycle != nil {
		s.incidentLifecycle.OperationStarted(ctx, *durable)
	}
	result, executeErr := s.registry.Execute(ctx, *durable)
	durable, persistErr := s.lifecycle.RecordExecution(ctx, durable.ID, result, executeErr)
	if persistErr != nil {
		return OperationExecutionResult{}, operationLedgerError(persistErr)
	}
	if s.incidentLifecycle != nil {
		s.incidentLifecycle.OperationCompleted(ctx, *durable, result, executeErr)
	}
	return operationResultFromLedger(*durable), executeErr
}

func (s *OperationService) lockOperation(ctx context.Context, operationID string) (func(), error) {
	s.operationLocksMu.Lock()
	if s.operationLocks == nil {
		s.operationLocks = map[string]chan struct{}{}
	}
	gate := s.operationLocks[operationID]
	if gate == nil {
		gate = make(chan struct{}, 1)
		s.operationLocks[operationID] = gate
	}
	s.operationLocksMu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *OperationService) Get(ctx context.Context, id string) (*ControlledOperation, error) {
	if s.lifecycle == nil {
		return nil, s.ledgerErr
	}
	return s.lifecycle.repository.Get(ctx, id)
}

func (s *OperationService) ApproveRecovery(ctx context.Context, op ControlledOperation, planID string) (*ControlledOperation, error) {
	if s.ledgerErr != nil || s.lifecycle == nil {
		return nil, operationLedgerError(fmt.Errorf("%s", firstNonEmpty(errorText(s.ledgerErr), "durable operation ledger is unavailable")))
	}
	durable, err := s.lifecycle.Create(ctx, op)
	if err != nil {
		return nil, err
	}
	return s.lifecycle.ApproveRecovery(ctx, durable.ID, planID, time.Now().UTC(), "")
}

func (s *OperationService) RecordVerification(ctx context.Context, id string, verification OperationVerificationState) (*ControlledOperation, error) {
	if s.lifecycle == nil {
		return nil, s.ledgerErr
	}
	return s.lifecycle.RecordVerification(ctx, id, verification)
}

func (s *OperationService) ReconcileInFlight(ctx context.Context, id string) (*ControlledOperation, error) {
	if s.lifecycle == nil {
		return nil, s.ledgerErr
	}
	return s.lifecycle.ReconcileInFlight(ctx, id)
}

// ReconcileInFlightOperations is startup-only recovery. It observes durable
// in-flight records and never invokes an executor.
func (s *OperationService) ReconcileInFlightOperations(ctx context.Context) {
	if s.lifecycle == nil {
		return
	}
	operations, err := s.lifecycle.repository.List(ctx, OperationListQuery{States: []OperationLifecycleState{OperationStateExecuting, OperationStateSucceeded, OperationStateVerifying, OperationStateRecovering}})
	if err != nil {
		log.Printf("operation startup reconciliation skipped: %v", err)
		return
	}
	for _, operation := range operations {
		if _, err := s.lifecycle.ReconcileInFlight(ctx, operation.ID); err != nil {
			log.Printf("operation startup reconciliation failed: operation=%s error=%v", operation.ID, err)
		}
	}
}

func (s *OperationService) reconcileExisting(ctx context.Context, operation ControlledOperation) (OperationExecutionResult, error) {
	if operation.State == OperationStateExecuting || operation.State == OperationStateUnknown {
		updated, err := s.lifecycle.ReconcileInFlight(ctx, operation.ID)
		if err != nil {
			return OperationExecutionResult{}, operationLedgerError(err)
		}
		return operationResultFromLedger(*updated), nil
	}
	return operationResultFromLedger(operation), nil
}

func operationResultFromLedger(operation ControlledOperation) OperationExecutionResult {
	execution := operation.Execution
	if execution.Status == "" {
		switch operation.State {
		case OperationStateExecuting:
			execution.Status = "EXECUTING"
		case OperationStateUnknown:
			execution.Status = "UNKNOWN"
		case OperationStateFailed:
			execution.Status = "FAILED"
		case OperationStateBlocked:
			execution.Status = "BLOCKED"
		case OperationStateSucceeded, OperationStateVerifying, OperationStateRecovering, OperationStateRecovered:
			execution.Status = "SUCCEEDED"
		}
	}
	return OperationExecutionResult{Execution: execution, ExternalTarget: execution.ExternalTarget, ExpectedReplicas: execution.ExpectedReplicas, PostState: execution.PostState, ActionVerified: execution.ActionVerified}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstOperationReason(reasons []string, fallback string) string {
	if len(reasons) > 0 && reasons[0] != "" {
		return reasons[0]
	}
	return fallback
}
