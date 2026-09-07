package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationLifecycleCreatesImmutableRestartIntentOnce(t *testing.T) {
	lifecycle, repo := newOperationLifecycleHarness(t, &fakeOperationInspector{})
	defer repo.Close()
	lifecycle.now = func() time.Time { return time.Date(2026, 9, 7, 1, 2, 3, 0, time.UTC) }
	op := testLedgerOperation(OperationRestartWorkload)
	created, err := lifecycle.Create(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if created.ExecutionIntent.RestartAt != "2026-09-07T01:02:03Z" {
		t.Fatalf("restart intent = %#v", created.ExecutionIntent)
	}
	lifecycle.now = func() time.Time { return time.Date(2026, 9, 7, 2, 3, 4, 0, time.UTC) }
	reloaded, err := lifecycle.Create(context.Background(), op)
	if err != nil || reloaded.ID != created.ID || reloaded.ExecutionIntent.RestartAt != created.ExecutionIntent.RestartAt {
		t.Fatalf("reload changed restart intent: %#v err=%v", reloaded, err)
	}
	events, _ := repo.ListEvents(context.Background(), created.ID)
	if operationEventCount(events, "OPERATION_CREATED") != 1 {
		t.Fatalf("reload duplicated operation creation event: %#v", events)
	}
}

func TestOperationLifecycleKeepsScaleIntentFixedOnReload(t *testing.T) {
	lifecycle, repo := newOperationLifecycleHarness(t, &fakeOperationInspector{})
	defer repo.Close()
	op := testLedgerOperation(OperationScaleWorkload)
	target := int64(6)
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, TargetReplicas: &target}
	created, err := lifecycle.Create(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := lifecycle.Create(context.Background(), op)
	if err != nil || reloaded.ExecutionIntent.TargetReplicas == nil || *reloaded.ExecutionIntent.TargetReplicas != 6 {
		t.Fatalf("scale target was recalculated: %#v err=%v", reloaded, err)
	}
	changed := op
	otherTarget := int64(9)
	changed.ExecutionIntent.TargetReplicas = &otherTarget
	if _, err := lifecycle.Create(context.Background(), changed); err == nil {
		t.Fatal("same operation ID accepted a changed scale target")
	}
	if created.ID != reloaded.ID {
		t.Fatalf("operation identity changed on reload: %s %s", created.ID, reloaded.ID)
	}
}

func TestOperationLifecycleTransitionsTerminalStatesAndDoesNotSpamEvents(t *testing.T) {
	lifecycle, repo := newOperationLifecycleHarness(t, &fakeOperationInspector{})
	defer repo.Close()
	op, err := lifecycle.Create(context.Background(), testLedgerOperation(OperationRestartWorkload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), op.ID, OperationStateReady, "READY", "ready", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), op.ID, OperationStateReady, "READY", "ready", nil); err != nil {
		t.Fatal(err)
	}
	events, _ := repo.ListEvents(context.Background(), op.ID)
	if operationEventCount(events, "READY") != 1 {
		t.Fatalf("duplicate lifecycle event: %#v", events)
	}
	for _, transition := range []struct {
		state OperationLifecycleState
		event string
	}{{OperationStateExecuting, "EXECUTION_STARTED"}, {OperationStateSucceeded, "EXECUTION_SUCCEEDED"}, {OperationStateVerifying, "VERIFICATION_STARTED"}, {OperationStateRecovered, "RECOVERED"}} {
		if _, err := lifecycle.Transition(context.Background(), op.ID, transition.state, transition.event, transition.event, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lifecycle.Transition(context.Background(), op.ID, OperationStateVerifying, "", "", nil); err == nil {
		t.Fatal("RECOVERED operation was not terminal")
	}
	blocked, err := lifecycle.Create(context.Background(), testLedgerOperationWithIncident(OperationRestartWorkload, "INC-BLOCKED"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), blocked.ID, OperationStateBlocked, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), blocked.ID, OperationStateReady, "", "", nil); err == nil {
		t.Fatal("BLOCKED operation was not terminal")
	}
}

func TestOperationLifecycleCrashRestartInspectionAppliedDoesNotExecute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	repo, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	restartAt := "2026-09-07T03:04:05Z"
	inspector := &restartIntentInspector{restartAt: restartAt}
	lifecycle := NewOperationLifecycleService(repo, inspector)
	op := testLedgerOperation(OperationRestartWorkload)
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, RestartAt: restartAt}
	created, err := lifecycle.Create(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), created.ID, OperationStateReady, "READY", "ready", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), created.ID, OperationStateExecuting, "EXECUTION_STARTED", "started", nil); err != nil {
		t.Fatal(err)
	}
	// Simulate watcher crash after the Kubernetes mutation but before an
	// execution result was persisted.
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewOperationLifecycleService(reopened, inspector)
	result, err := restarted.ReconcileInFlight(context.Background(), created.ID)
	if err != nil || result.State != OperationStateSucceeded || result.ExecutionIntent.RestartAt != restartAt || result.ID != created.ID {
		t.Fatalf("crash reconciliation = %#v err=%v", result, err)
	}
	events, _ := reopened.ListEvents(context.Background(), created.ID)
	if operationEventCount(events, "EXECUTION_EFFECT_OBSERVED") != 1 || operationEventCount(events, "EXECUTION_STARTED") != 1 {
		t.Fatalf("crash timeline = %#v", events)
	}
}

func TestOperationLifecycleScaleAndReleaseInspectAppliedDoNotExecute(t *testing.T) {
	t.Run("scale", func(t *testing.T) {
		lifecycle, repo := newOperationLifecycleHarness(t, &scaleIntentInspector{replicas: 5})
		defer repo.Close()
		op := testLedgerOperation(OperationScaleWorkload)
		target := int64(5)
		op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, TargetReplicas: &target}
		created := createExecutingOperation(t, lifecycle, op)
		result, err := lifecycle.ReconcileInFlight(context.Background(), created.ID)
		if err != nil || result.State != OperationStateSucceeded {
			t.Fatalf("scale inspection = %#v err=%v", result, err)
		}
	})
	t.Run("release", func(t *testing.T) {
		reportDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(reportDir, "runtime-action-execution-result-rel-1.json"), mustRuntimeActionResult(t, "rel-1", "ROLLBACK_ROLLOUT"), 0o600); err != nil {
			t.Fatal(err)
		}
		lifecycle, repo := newOperationLifecycleHarness(t, NewRuntimeActionExecutionResultInspector(reportDir))
		defer repo.Close()
		op := testLedgerReleaseOperation()
		created := createExecutingOperation(t, lifecycle, op)
		if created.ExecutionIntent.ReleaseID != "rel-1" || created.ExecutionIntent.RuntimeActionIdentity != created.ID || created.ExecutionIntent.Action != OperationRollbackRelease || created.ExecutionIntent.Target.ReleaseID != "rel-1" {
			t.Fatalf("release execution intent was not fixed to the canonical operation: %#v", created.ExecutionIntent)
		}
		result, err := lifecycle.ReconcileInFlight(context.Background(), created.ID)
		if err != nil || result.State != OperationStateSucceeded {
			t.Fatalf("release inspection = %#v err=%v", result, err)
		}
	})
}

func TestOperationLifecycleUnknownExternalStateNeverRetries(t *testing.T) {
	inspector := &fakeOperationInspector{result: OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "cluster is unavailable"}}
	lifecycle, repo := newOperationLifecycleHarness(t, inspector)
	defer repo.Close()
	created := createExecutingOperation(t, lifecycle, testLedgerOperation(OperationRestartWorkload))
	result, err := lifecycle.ReconcileInFlight(context.Background(), created.ID)
	if err != nil || result.State != OperationStateUnknown || inspector.calls != 1 {
		t.Fatalf("unknown inspection = %#v err=%v calls=%d", result, err, inspector.calls)
	}
	events, _ := repo.ListEvents(context.Background(), created.ID)
	if operationEventCount(events, "EXECUTION_STATE_UNKNOWN") != 1 {
		t.Fatalf("uncertainty was not recorded: %#v", events)
	}
}

func TestOperationLifecycleCrashRestartUnknownStateDoesNotRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	repo, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	restartAt := "2026-09-07T08:09:10Z"
	unknown := &fakeOperationInspector{result: OperationInspectionResult{Status: OperationInspectionUnknown, Reason: "Kubernetes API is unavailable"}}
	lifecycle := NewOperationLifecycleService(repo, unknown)
	op := testLedgerOperation(OperationRestartWorkload)
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, RestartAt: restartAt}
	created := createExecutingOperation(t, lifecycle, op)
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result, err := NewOperationLifecycleService(reopened, unknown).ReconcileInFlight(context.Background(), created.ID)
	if err != nil || result.State != OperationStateUnknown || result.ExecutionIntent.RestartAt != restartAt || unknown.calls != 1 {
		t.Fatalf("unknown crash recovery = %#v err=%v inspectCalls=%d", result, err, unknown.calls)
	}
	events, _ := reopened.ListEvents(context.Background(), created.ID)
	if operationEventCount(events, "EXECUTION_STATE_UNKNOWN") != 1 {
		t.Fatalf("unknown crash recovery did not record uncertainty: %#v", events)
	}
}

func newOperationLifecycleHarness(t *testing.T, inspector OperationExecutionInspector) (*OperationLifecycleService, *SQLiteOperationRepository) {
	t.Helper()
	repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	return NewOperationLifecycleService(repo, inspector), repo
}

func createExecutingOperation(t *testing.T, lifecycle *OperationLifecycleService, operation ControlledOperation) *ControlledOperation {
	t.Helper()
	created, err := lifecycle.Create(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), created.ID, OperationStateReady, "READY", "ready", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Transition(context.Background(), created.ID, OperationStateExecuting, "EXECUTION_STARTED", "started", nil); err != nil {
		t.Fatal(err)
	}
	return created
}

func testLedgerOperation(action OperationAction) ControlledOperation {
	return testLedgerOperationWithIncident(action, "INC-1")
}

func testLedgerOperationWithIncident(action OperationAction, incidentID string) ControlledOperation {
	target := OperationTarget{Service: "demo-app", Namespace: "slo-rollout", WorkloadKind: "Rollout", WorkloadName: "demo-app"}
	source := OperationSource{Type: "INCIDENT", ID: incidentID}
	subject := OperationSubject{Type: "SERVICE", ID: "demo-app"}
	id := BuildOperationID(source, subject, action, target)
	return ControlledOperation{ID: id, IdempotencyKey: id, Source: source, Subject: subject, Action: action, Target: target, Policy: OperationPolicyState{Decision: "ALLOW"}, Approval: OperationApprovalState{Approved: true}, Preflight: OperationPreflightState{Status: "READY"}}
}

func testLedgerReleaseOperation() ControlledOperation {
	target := OperationTarget{Service: "demo-app", ReleaseID: "rel-1"}
	source := OperationSource{Type: "INCIDENT", ID: "INC-1"}
	subject := OperationSubject{Type: "RELEASE", ID: "rel-1"}
	id := BuildOperationID(source, subject, OperationRollbackRelease, target)
	return ControlledOperation{ID: id, IdempotencyKey: id, Source: source, Subject: subject, Action: OperationRollbackRelease, Target: target, Policy: OperationPolicyState{Decision: "ALLOW"}, Approval: OperationApprovalState{Approved: true}, Preflight: OperationPreflightState{Status: "READY"}}
}

func mustRuntimeActionResult(t *testing.T, releaseID, action string) []byte {
	t.Helper()
	document := map[string]interface{}{
		"runtimeActionExecutionResultId": "RAR-1",
		"release":                        map[string]interface{}{"releaseId": releaseID},
		"action":                         map[string]interface{}{"requestedAction": action},
		"result":                         map[string]interface{}{"executionStatus": "SUCCEEDED"},
		"verificationSummary":            map[string]interface{}{"verified": true},
		"postActionVerification":         map[string]interface{}{"verificationStatus": "VERIFIED"},
	}
	bytes, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

func operationEventCount(events []OperationTimelineEvent, wanted string) int {
	count := 0
	for _, event := range events {
		if event.Type == wanted {
			count++
		}
	}
	return count
}

type fakeOperationInspector struct {
	result OperationInspectionResult
	err    error
	calls  int
}

func (i *fakeOperationInspector) Inspect(context.Context, ControlledOperation) (OperationInspectionResult, error) {
	i.calls++
	return i.result, i.err
}

// These fakes model the external Kubernetes facts relevant to crash recovery.
// They are deliberately read-only; the lifecycle owns no Execute dependency.
type restartIntentInspector struct {
	restartAt string
	calls     int
}

func (i *restartIntentInspector) Inspect(_ context.Context, operation ControlledOperation) (OperationInspectionResult, error) {
	i.calls++
	if operation.Action == OperationRestartWorkload && operation.ExecutionIntent.RestartAt == i.restartAt {
		return OperationInspectionResult{Status: OperationInspectionApplied, Reason: "fake Rollout spec.restartAt matches persisted intent"}, nil
	}
	return OperationInspectionResult{Status: OperationInspectionNotApplied, Reason: "fake Rollout restart intent differs"}, nil
}

type scaleIntentInspector struct{ replicas int64 }

func (i *scaleIntentInspector) Inspect(_ context.Context, operation ControlledOperation) (OperationInspectionResult, error) {
	if operation.Action == OperationScaleWorkload && operation.ExecutionIntent.TargetReplicas != nil && *operation.ExecutionIntent.TargetReplicas == i.replicas {
		return OperationInspectionResult{Status: OperationInspectionApplied, Reason: "fake Rollout spec.replicas matches persisted intent"}, nil
	}
	return OperationInspectionResult{Status: OperationInspectionNotApplied, Reason: "fake Rollout replicas differ"}, nil
}
