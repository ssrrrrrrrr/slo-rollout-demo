package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeIncidentWithoutReleaseCanRestart(t *testing.T) {
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	if err := os.MkdirAll(filepath.Join(repoDir, "configs", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	book := `apiVersion: sentinel.io/v1alpha1
kind: Runbook
metadata: {name: restart-unhealthy-workload}
spec:
  match: {runtimeStatus: [UNHEALTHY], requireRelease: false}
  action: {type: RESTART_WORKLOAD}
  risk: {level: MEDIUM}
  approval: {required: true}
  verification: {runtimeStatus: HEALTHY}
`
	if err := os.WriteFile(filepath.Join(repoDir, "configs", "runbooks", "restart.runbook.yaml"), []byte(book), 0o600); err != nil {
		t.Fatal(err)
	}
	services := NewServiceService(repoDir, nil)
	slo := NewSLOService(repoDir, services, &remediationSLOProvider{status: SLOStatusHealthy})
	runtime := NewRuntimeService(services, &remediationRuntimeProvider{status: RuntimeStatusUnhealthy})
	incidents := NewIncidentService(services, slo, runtime, NewReliabilityIncidentDetector())
	executor := &fakeRecoveryExecutor{}
	recovery := NewRecoveryService(incidents, runtime, services, repoDir, executor)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	incident, err := incidents.ActiveForService(context.Background(), request, "demo-app")
	if err != nil || incident == nil || incident.RelatedRelease != nil {
		t.Fatalf("expected runtime incident with no release: %#v err=%v", incident, err)
	}
	plan, err := recovery.Plan(context.Background(), request, incident.ID)
	if err != nil || plan.Action.Type != RecoveryRestartWorkload || plan.Target.Name != "demo-app" || plan.Status != RecoveryPlanBlocked {
		t.Fatalf("unexpected recovery plan: %#v err=%v", plan, err)
	}
	t.Setenv("S_SENTINEL_RECOVERY_ENABLED", "true")
	t.Setenv("S_SENTINEL_ALLOW_RECOVERY_RESTART_WORKLOAD", "true")
	plan, err = recovery.Plan(context.Background(), request, incident.ID)
	if err != nil || plan.Status != RecoveryPlanBlocked || plan.Preflight.Eligible {
		t.Fatalf("approval must still be required: %#v err=%v", plan, err)
	}
	if _, err = recovery.Execute(context.Background(), request, incident.ID, plan.ID); remediationErrorStatus(err) != 409 || executor.calls != 0 {
		t.Fatalf("execute must require approval: %v", err)
	}
	if _, err = recovery.Approve(context.Background(), request, incident.ID, "RP-wrong"); remediationErrorStatus(err) != 400 {
		t.Fatalf("mismatched plan cannot approve: %v", err)
	}
	plan, err = recovery.Approve(context.Background(), request, incident.ID, plan.ID)
	if err != nil || plan.Status != RecoveryPlanReady || !plan.Approval.Approved {
		t.Fatalf("current plan was not approved: %#v err=%v", plan, err)
	}
	if _, err = recovery.Preview(context.Background(), request, incident.ID); err != nil || executor.calls != 0 {
		t.Fatalf("preview mutated: calls=%d err=%v", executor.calls, err)
	}
	executed, err := recovery.Execute(context.Background(), request, incident.ID, plan.ID)
	if err != nil || executor.calls != 1 || executed.Execution == nil || executed.Execution.Action != RecoveryRestartWorkload {
		t.Fatalf("restart executor was not invoked: %#v err=%v calls=%d", executed, err, executor.calls)
	}
	duplicate, err := recovery.Execute(context.Background(), request, incident.ID, plan.ID)
	if err != nil || executor.calls != 1 || duplicate.Execution == nil || !duplicate.Execution.Idempotent {
		t.Fatalf("duplicate execute is not safe: %#v err=%v calls=%d", duplicate, err, executor.calls)
	}
}

func TestRecoveryApprovalDoesNotBypassGatesOrDrift(t *testing.T) {
	h := newRecoveryHarness(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	incident := h.incident(t)
	plan, err := h.recovery.Plan(context.Background(), request, incident.ID)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := h.recovery.Approve(context.Background(), request, incident.ID, plan.ID)
	if err != nil || !approved.Approval.Approved {
		t.Fatalf("approve: %#v %v", approved, err)
	}
	if _, err := h.recovery.Execute(context.Background(), request, incident.ID, plan.ID); remediationErrorStatus(err) != 409 || h.executor.calls != 0 {
		t.Fatalf("approval must not bypass global gate: %v", err)
	}
	// A target change creates a new deterministic plan ID, so the old approval
	// cannot authorize it.
	h.services.configDir = h.changedConfigDir(t)
	changed, err := h.recovery.Plan(context.Background(), request, incident.ID)
	if err != nil || changed.ID == plan.ID || changed.Approval.Approved {
		t.Fatalf("approval drifted to changed plan: %#v err=%v", changed, err)
	}
}

type recoveryHarness struct {
	recovery  *RecoveryService
	incidents *IncidentService
	services  *ServiceService
	executor  *fakeRecoveryExecutor
	repoDir   string
}

func newRecoveryHarness(t *testing.T) *recoveryHarness {
	t.Helper()
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	if err := os.MkdirAll(filepath.Join(repoDir, "configs", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	book := `apiVersion: sentinel.io/v1alpha1
kind: Runbook
metadata: {name: restart-unhealthy-workload}
spec:
  match: {runtimeStatus: [UNHEALTHY], requireRelease: false}
  action: {type: RESTART_WORKLOAD}
  approval: {required: true}
`
	if err := os.WriteFile(filepath.Join(repoDir, "configs", "runbooks", "restart.runbook.yaml"), []byte(book), 0o600); err != nil {
		t.Fatal(err)
	}
	services := NewServiceService(repoDir, nil)
	slo := NewSLOService(repoDir, services, &remediationSLOProvider{status: SLOStatusHealthy})
	runtime := NewRuntimeService(services, &remediationRuntimeProvider{status: RuntimeStatusUnhealthy})
	incidents := NewIncidentService(services, slo, runtime, NewReliabilityIncidentDetector())
	executor := &fakeRecoveryExecutor{}
	return &recoveryHarness{NewRecoveryService(incidents, runtime, services, repoDir, executor), incidents, services, executor, repoDir}
}
func (h *recoveryHarness) incident(t *testing.T) *ReliabilityIncident {
	t.Helper()
	i, e := h.incidents.ActiveForService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
	if e != nil || i == nil {
		t.Fatalf("incident: %#v %v", i, e)
	}
	return i
}
func (h *recoveryHarness) changedConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "configs", "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(testServiceConfig, "namespace: slo-rollout", "namespace: changed-namespace", 1)
	if err := os.WriteFile(filepath.Join(dir, "configs", "services", "demo.service.yaml"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "configs", "services")
}

func TestRunbookMatcherSafety(t *testing.T) {
	books := []Runbook{{}}
	books[0].Metadata.Name = "restart"
	books[0].Spec.Action.Type = RecoveryRestartWorkload
	books[0].Spec.Match.RuntimeStatus = []RuntimeStatus{RuntimeStatusUnhealthy}
	for _, state := range []RuntimeStatus{RuntimeStatusDegraded, RuntimeStatusHealthy} {
		incident := ReliabilityIncident{Runtime: RuntimeSnapshot{Status: state}}
		if got := (DeterministicRecoveryPlanner{}).Plan(incident, Service{}, books); got != nil {
			t.Fatalf("restart must not match %s", state)
		}
	}
}

type fakeRecoveryExecutor struct{ calls int }

func (e *fakeRecoveryExecutor) Supports(action RecoveryActionType) bool {
	return action == RecoveryRestartWorkload || action == RecoveryScaleWorkload
}
func (e *fakeRecoveryExecutor) Preflight(context.Context, RecoveryPlan) error { return nil }
func (e *fakeRecoveryExecutor) Execute(_ context.Context, plan RecoveryPlan) (int64, error) {
	e.calls++
	return plan.Action.Parameters.MaxReplicas, nil
}
