package main

import (
	"context"
	"testing"
)

func TestBuildOperationIDIsDeterministicAndBoundToActionTarget(t *testing.T) {
	source := OperationSource{Type: "RUNBOOK", ID: "INC-1"}
	subject := OperationSubject{Type: "SERVICE", ID: "demo-app"}
	target := OperationTarget{Service: "demo-app", Namespace: "slo-rollout", WorkloadKind: "Rollout", WorkloadName: "demo-app"}
	first := BuildOperationID(source, subject, OperationRestartWorkload, target)
	if first == "" || first != BuildOperationID(source, subject, OperationRestartWorkload, target) {
		t.Fatalf("operation identity is not deterministic: %q", first)
	}
	if first == BuildOperationID(source, subject, OperationScaleWorkload, target) {
		t.Fatal("action change must change operation identity")
	}
	target.WorkloadName = "other"
	if first == BuildOperationID(source, subject, OperationRestartWorkload, target) {
		t.Fatal("target change must change operation identity")
	}
}

func TestOperationServiceGatesAndIdempotency(t *testing.T) {
	executor := &fakeOperationExecutor{}
	service := NewOperationService(NewOperationExecutorRegistry(executor))
	op := testControlledOperation()
	op.Preflight.Status = "BLOCKED"
	op.Preflight.BlockedReasons = []string{"approval is missing"}
	result, err := service.Execute(context.Background(), op)
	if err != nil || result.Execution.Status != "BLOCKED" || executor.calls != 0 {
		t.Fatalf("blocked operation invoked executor: %#v err=%v calls=%d", result, err, executor.calls)
	}
	op.Preflight.Status, op.Preflight.BlockedReasons = "READY", nil
	op.Approval = OperationApprovalState{Required: true, Approved: false}
	result, err = service.Execute(context.Background(), op)
	if err != nil || result.Execution.Status != "BLOCKED" || executor.calls != 0 {
		t.Fatalf("unapproved operation invoked executor: %#v err=%v calls=%d", result, err, executor.calls)
	}
	op.Approval.Approved = true
	result, err = service.Execute(context.Background(), op)
	if err != nil || result.Execution.Status != "SUCCEEDED" || executor.calls != 1 {
		t.Fatalf("ready operation did not execute: %#v err=%v calls=%d", result, err, executor.calls)
	}
	_, err = service.Execute(context.Background(), op)
	if err != nil || executor.calls != 1 {
		t.Fatalf("duplicate operation was executed twice: err=%v calls=%d", err, executor.calls)
	}
}

func TestOperationAdaptersPreserveExistingContracts(t *testing.T) {
	releaseAdapter := &fakeOperationRemediationAdapter{result: RuntimeActionExecutionProjection{ResultID: "RAR-1", Status: "SUCCEEDED", ActionVerified: true, Target: RemediationTarget{ReleaseID: "rel-1", Namespace: "ns", Workload: "demo"}}}
	release := ReleaseRuntimeActionExecutorAdapter{adapter: releaseAdapter}
	result, err := release.Execute(context.Background(), ControlledOperation{Action: OperationRollbackRelease, Target: OperationTarget{ReleaseID: "rel-1"}})
	if err != nil || releaseAdapter.action != "ROLLBACK" || result.Execution.ExternalResultID != "RAR-1" || !result.ActionVerified {
		t.Fatalf("release result was not projected from existing adapter: %#v err=%v action=%s", result, err, releaseAdapter.action)
	}
	kubeExecutor := &fakeOperationRecoveryExecutor{}
	kube := KubernetesRecoveryExecutorAdapter{executor: kubeExecutor}
	_, err = kube.Execute(context.Background(), ControlledOperation{Action: OperationScaleWorkload, Target: OperationTarget{Namespace: "ns", WorkloadKind: "Rollout", WorkloadName: "demo"}, Parameters: RecoveryActionParameters{Direction: "UP", Step: 2, MaxReplicas: 5}})
	if err != nil || kubeExecutor.plan.Action.Type != RecoveryScaleWorkload || kubeExecutor.plan.Action.Parameters.Step != 2 {
		t.Fatalf("kubernetes adapter did not preserve bounded recovery plan: %#v err=%v", kubeExecutor.plan, err)
	}
}

func TestPlanProjectionsUseOneOperationIdentity(t *testing.T) {
	remediation := RemediationPlan{IncidentID: "INC-1", Service: "demo-app", Target: RemediationTarget{ReleaseID: "rel-1"}, Recommendation: RemediationAction{Action: "ROLLBACK"}, Policy: RemediationPolicy{Decision: "ALLOW"}, Eligibility: RemediationEligibility{Eligible: true}}
	remediationOp := remediationControlledOperation(remediation)
	if remediationOp.Subject.Type != "RELEASE" || remediationOp.Action != OperationRollbackRelease || remediationOp.Preflight.Status != "READY" {
		t.Fatalf("unexpected remediation projection: %#v", remediationOp)
	}
	recovery := RecoveryPlan{ID: "RP-1", IncidentID: "INC-1", Service: "demo-app", ReleaseID: "rel-1", Action: RunbookAction{Type: RecoveryRollbackRelease}, Target: RecoveryTarget{Namespace: "ns", Kind: "Rollout", Name: "demo"}, Policy: RemediationPolicy{Decision: "REQUIRE_APPROVAL"}, Approval: RemediationApproval{Required: true, Approved: true}, Preflight: RemediationEligibility{Eligible: true}}
	recoveryOp := recoveryControlledOperation(recovery, &RecoveryApprovalState{PlanID: "RP-1", Approved: true}, &remediation)
	if recoveryOp.Subject.Type != "RELEASE" || recoveryOp.ID != BuildOperationID(recoveryOp.Source, recoveryOp.Subject, recoveryOp.Action, recoveryOp.Target) || len(recoveryOp.Approval.RequiredChecks) != 2 {
		t.Fatalf("unexpected recovery rollback projection: %#v", recoveryOp)
	}
	if recoveryOp.ID != remediationOp.ID {
		t.Fatalf("rollback reached from recovery must use the same operation identity: recovery=%s remediation=%s", recoveryOp.ID, remediationOp.ID)
	}
	if recoveryOp.IdempotencyKey != recoveryOp.ID {
		t.Fatalf("operation idempotency is not canonical: %#v", recoveryOp)
	}
}

type fakeOperationExecutor struct{ calls int }

func (e *fakeOperationExecutor) Supports(OperationAction) bool { return true }
func (e *fakeOperationExecutor) Execute(context.Context, ControlledOperation) (OperationExecutionResult, error) {
	e.calls++
	return OperationExecutionResult{Execution: OperationExecutionState{Status: "SUCCEEDED", Executor: "fake"}}, nil
}

type fakeOperationRemediationAdapter struct {
	action string
	result RuntimeActionExecutionProjection
}

func (a *fakeOperationRemediationAdapter) Available(RemediationExecutionRequest) error { return nil }
func (a *fakeOperationRemediationAdapter) Execute(_ context.Context, request RemediationExecutionRequest) (RuntimeActionExecutionProjection, error) {
	a.action = request.Action
	return a.result, nil
}

type fakeOperationRecoveryExecutor struct{ plan RecoveryPlan }

func (e *fakeOperationRecoveryExecutor) Supports(RecoveryActionType) bool { return true }
func (e *fakeOperationRecoveryExecutor) Preflight(_ context.Context, plan RecoveryPlan) error {
	e.plan = plan
	return nil
}
func (e *fakeOperationRecoveryExecutor) Execute(_ context.Context, plan RecoveryPlan) (int64, error) {
	e.plan = plan
	return 5, nil
}

func testControlledOperation() ControlledOperation {
	target := OperationTarget{Service: "demo-app", Namespace: "ns", WorkloadKind: "Rollout", WorkloadName: "demo"}
	source := OperationSource{Type: "RUNBOOK", ID: "INC-1"}
	subject := OperationSubject{Type: "SERVICE", ID: "demo-app"}
	id := BuildOperationID(source, subject, OperationRestartWorkload, target)
	return ControlledOperation{ID: id, IdempotencyKey: id, Source: source, Subject: subject, Action: OperationRestartWorkload, Target: target, Policy: OperationPolicyState{Decision: "ALLOW"}, Approval: OperationApprovalState{Approved: true}, Preflight: OperationPreflightState{Status: "READY"}}
}
