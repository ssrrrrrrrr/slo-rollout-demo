package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestDurableOperationExecutionPersistsIntentBeforeExecutorAndDeduplicates(t *testing.T) {
	repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	executor := &cutoverExecutor{repo: repo}
	service := NewDurableOperationService(NewOperationExecutorRegistry(executor), NewOperationLifecycleService(repo, &fakeOperationInspector{}))
	op := testLedgerOperation(OperationRestartWorkload)
	result, err := service.Execute(context.Background(), op)
	if err != nil || result.Execution.Status != "SUCCEEDED" || executor.calls != 1 || executor.observedState != OperationStateExecuting || executor.operation.ExecutionIntent.RestartAt == "" {
		t.Fatalf("durable restart execution = %#v err=%v executor=%#v", result, err, executor)
	}
	persisted, err := repo.Get(context.Background(), op.ID)
	if err != nil || persisted.ExecutionIntent.RestartAt != executor.operation.ExecutionIntent.RestartAt || persisted.State != OperationStateSucceeded {
		t.Fatalf("persisted restart operation = %#v err=%v", persisted, err)
	}
	_, err = service.Execute(context.Background(), op)
	if err != nil || executor.calls != 1 {
		t.Fatalf("duplicate execute invoked mutation: err=%v calls=%d", err, executor.calls)
	}
}

func TestDurableOperationExecutionUsesFrozenScaleAndReleaseIntent(t *testing.T) {
	for _, testcase := range []struct {
		name string
		op   ControlledOperation
		want OperationAction
	}{
		{name: "scale", op: func() ControlledOperation {
			op := testLedgerOperation(OperationScaleWorkload)
			current, target := int64(3), int64(5)
			op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, InitialReplicas: &current, TargetReplicas: &target}
			return op
		}(), want: OperationScaleWorkload},
		{name: "release", op: testLedgerReleaseOperation(), want: OperationRollbackRelease},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			executor := &cutoverExecutor{repo: repo}
			service := NewDurableOperationService(NewOperationExecutorRegistry(executor), NewOperationLifecycleService(repo, &fakeOperationInspector{}))
			if _, err := service.Execute(context.Background(), testcase.op); err != nil {
				t.Fatal(err)
			}
			if executor.calls != 1 || executor.operation.Action != testcase.want {
				t.Fatalf("executor invocation = %#v", executor)
			}
			if testcase.want == OperationScaleWorkload && (executor.operation.ExecutionIntent.TargetReplicas == nil || *executor.operation.ExecutionIntent.TargetReplicas != 5) {
				t.Fatalf("executor did not receive frozen scale target: %#v", executor.operation.ExecutionIntent)
			}
			if testcase.want == OperationRollbackRelease && (executor.operation.ExecutionIntent.ReleaseID != "rel-1" || executor.operation.ExecutionIntent.RuntimeActionIdentity != executor.operation.ID) {
				t.Fatalf("executor did not receive frozen release intent: %#v", executor.operation.ExecutionIntent)
			}
		})
	}
}

func TestDurableOperationDatabaseFailurePreventsExecutor(t *testing.T) {
	executor := &cutoverExecutor{}
	lifecycle := NewOperationLifecycleService(failingCreateOperationRepository{}, &fakeOperationInspector{})
	service := NewDurableOperationService(NewOperationExecutorRegistry(executor), lifecycle)
	_, err := service.Execute(context.Background(), testLedgerOperation(OperationRestartWorkload))
	if err == nil || executor.calls != 0 {
		t.Fatalf("database failure reached executor: err=%v calls=%d", err, executor.calls)
	}
}

func TestDurableOperationConcurrentDuplicateExecutesOnce(t *testing.T) {
	repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	executor := &blockingCutoverExecutor{entered: make(chan struct{}), release: make(chan struct{})}
	service := NewDurableOperationService(NewOperationExecutorRegistry(executor), NewOperationLifecycleService(repo, &fakeOperationInspector{}))
	op := testLedgerOperation(OperationRestartWorkload)
	done := make(chan error, 2)
	go func() { _, err := service.Execute(context.Background(), op); done <- err }()
	<-executor.entered
	go func() { _, err := service.Execute(context.Background(), op); done <- err }()
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("concurrent duplicate execution calls = %d", executor.calls)
	}
}

func TestDurableRecoveryApprovalSurvivesReopenAndBindsOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	repo, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	executor := &cutoverExecutor{repo: repo}
	service := NewDurableOperationService(NewOperationExecutorRegistry(executor), NewOperationLifecycleService(repo, &fakeOperationInspector{}))
	op := testLedgerOperation(OperationRestartWorkload)
	op.Approval = operationApproval(op.ID, []OperationApprovalCheck{{Type: "RECOVERY_APPROVAL", SubjectID: "RP-1", Required: true}})
	if _, err := service.ApproveRecovery(context.Background(), op, "RP-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restartedExecutor := &cutoverExecutor{repo: reopened}
	restarted := NewDurableOperationService(NewOperationExecutorRegistry(restartedExecutor), NewOperationLifecycleService(reopened, &fakeOperationInspector{}))
	if _, err := restarted.Execute(context.Background(), op); err != nil || restartedExecutor.calls != 1 {
		t.Fatalf("durable approval did not survive restart: err=%v calls=%d", err, restartedExecutor.calls)
	}
	changed := op
	changed.Target.WorkloadName = "other"
	changed.ID, changed.IdempotencyKey = BuildOperationID(changed.Source, changed.Subject, changed.Action, changed.Target), BuildOperationID(changed.Source, changed.Subject, changed.Action, changed.Target)
	if _, err := restarted.Execute(context.Background(), changed); err != nil || restartedExecutor.calls != 1 {
		t.Fatalf("approval drift reached executor: err=%v calls=%d", err, restartedExecutor.calls)
	}
}

func TestDurableExecutingReconstructionInspectsWithoutExecute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	repo, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewOperationLifecycleService(repo, &fakeOperationInspector{result: OperationInspectionResult{Status: OperationInspectionApplied, Reason: "effect observed"}})
	created := createExecutingOperation(t, lifecycle, testLedgerOperation(OperationRestartWorkload))
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	executor := &cutoverExecutor{repo: reopened}
	restarted := NewDurableOperationService(NewOperationExecutorRegistry(executor), NewOperationLifecycleService(reopened, &fakeOperationInspector{result: OperationInspectionResult{Status: OperationInspectionApplied}}))
	result, err := restarted.Execute(context.Background(), testLedgerOperation(OperationRestartWorkload))
	if err != nil || result.Execution.Status != "SUCCEEDED" || executor.calls != 0 {
		t.Fatalf("reconstructed EXECUTING operation re-executed: %#v err=%v calls=%d", result, err, executor.calls)
	}
	persisted, _ := reopened.Get(context.Background(), created.ID)
	if persisted.State != OperationStateSucceeded {
		t.Fatalf("reconstructed state = %s", persisted.State)
	}
}

func TestDurableVerificationSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	repo, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	executor := &cutoverExecutor{repo: repo}
	service := NewDurableOperationService(NewOperationExecutorRegistry(executor), NewOperationLifecycleService(repo, &fakeOperationInspector{}))
	op := testLedgerOperation(OperationRestartWorkload)
	if _, err := service.Execute(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordVerification(context.Background(), op.ID, OperationVerificationState{Status: "RECOVERING", Reason: "rollout is converging"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewDurableOperationService(NewOperationExecutorRegistry(&cutoverExecutor{repo: reopened}), NewOperationLifecycleService(reopened, &fakeOperationInspector{}))
	persisted, err := restarted.Get(context.Background(), op.ID)
	if err != nil || persisted.State != OperationStateRecovering || persisted.Verification.Status != "RECOVERING" {
		t.Fatalf("durable verification after restart = %#v err=%v", persisted, err)
	}
	if _, err := restarted.RecordVerification(context.Background(), op.ID, OperationVerificationState{Status: "RECOVERED", Reason: "runtime healthy"}); err != nil {
		t.Fatal(err)
	}
	persisted, _ = restarted.Get(context.Background(), op.ID)
	if persisted.State != OperationStateRecovered {
		t.Fatalf("durable verification did not recover operation: %#v", persisted)
	}
}

func TestStartupInFlightReconciliationInspectsWithoutMutation(t *testing.T) {
	repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	inspector := &fakeOperationInspector{result: OperationInspectionResult{Status: OperationInspectionApplied, Reason: "effect observed"}}
	lifecycle := NewOperationLifecycleService(repo, inspector)
	operation := createExecutingOperation(t, lifecycle, testLedgerOperation(OperationRestartWorkload))
	executor := &cutoverExecutor{repo: repo}
	service := NewDurableOperationService(NewOperationExecutorRegistry(executor), lifecycle)
	service.ReconcileInFlightOperations(context.Background())
	persisted, _ := repo.Get(context.Background(), operation.ID)
	if inspector.calls != 1 || executor.calls != 0 || persisted.State != OperationStateSucceeded {
		t.Fatalf("startup recovery = inspector=%d executor=%d operation=%#v", inspector.calls, executor.calls, persisted)
	}
}

type cutoverExecutor struct {
	repo          OperationRepository
	calls         int
	observedState OperationLifecycleState
	operation     ControlledOperation
}

func (e *cutoverExecutor) Supports(OperationAction) bool { return true }
func (e *cutoverExecutor) Execute(ctx context.Context, operation ControlledOperation) (OperationExecutionResult, error) {
	e.calls++
	e.operation = operation
	if e.repo != nil {
		persisted, err := e.repo.Get(ctx, operation.ID)
		if err != nil {
			return OperationExecutionResult{}, err
		}
		e.observedState = persisted.State
	}
	return OperationExecutionResult{Execution: OperationExecutionState{Status: "SUCCEEDED", Executor: "cutover-test"}}, nil
}

type failingCreateOperationRepository struct{ OperationRepository }

func (failingCreateOperationRepository) Get(context.Context, string) (*ControlledOperation, error) {
	return nil, &OperationNotFoundError{}
}
func (failingCreateOperationRepository) Create(context.Context, ControlledOperation) error {
	return errors.New("operation database write failed")
}

type blockingCutoverExecutor struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   int
}

func (e *blockingCutoverExecutor) Supports(OperationAction) bool { return true }
func (e *blockingCutoverExecutor) Execute(context.Context, ControlledOperation) (OperationExecutionResult, error) {
	e.calls++
	e.once.Do(func() { close(e.entered) })
	<-e.release
	return OperationExecutionResult{Execution: OperationExecutionState{Status: "SUCCEEDED", Executor: "blocking-cutover-test"}}, nil
}
