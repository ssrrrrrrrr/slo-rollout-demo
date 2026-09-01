package main

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"hash/fnv"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RecoveryExecutor interface {
	Supports(RecoveryActionType) bool
	Preflight(context.Context, RecoveryPlan) error
	Execute(context.Context, RecoveryPlan) (int64, error)
}
type RecoveryService struct {
	incidentService *IncidentService
	runtimeService  *RuntimeService
	services        *ServiceService
	runbookDir      string
	planner         RecoveryPlanner
	executor        RecoveryExecutor
	rollback        *RemediationService
	mu              sync.Mutex
	executions      map[string]RecoveryExecution
	approvals       map[string]RecoveryApprovalState
}

func NewRecoveryService(incidents *IncidentService, runtime *RuntimeService, services *ServiceService, repoDir string, executor RecoveryExecutor) *RecoveryService {
	return &RecoveryService{incidentService: incidents, runtimeService: runtime, services: services, runbookDir: filepath.Join(repoDir, "configs", "runbooks"), planner: DeterministicRecoveryPlanner{}, executor: executor, rollback: NewRemediationService(incidents, incidents.sloService, runtime, services.repository, NewRuntimeActionPipelineAdapter(repoDir, filepath.Join(repoDir, "docs", "release-reports"))), executions: map[string]RecoveryExecution{}, approvals: map[string]RecoveryApprovalState{}}
}
func (api *portalAPI) recoveryService() *RecoveryService {
	if api.recoverySvc != nil {
		return api.recoverySvc
	}
	return NewRecoveryService(api.incidentService(), api.runtimeService(), api.serviceService(), api.cfg.RepoDir, NewKubernetesRecoveryExecutor())
}
func (svc *RecoveryService) Load() ([]Runbook, error) {
	files, err := filepath.Glob(filepath.Join(svc.runbookDir, "*.runbook.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	result := make([]Runbook, 0, len(files))
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			return nil, e
		}
		var r Runbook
		if e = yaml.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		if e = validateRunbook(r); e != nil {
			return nil, fmt.Errorf("%s: %w", f, e)
		}
		result = append(result, r)
	}
	return result, nil
}
func validateRunbook(r Runbook) error {
	if r.APIVersion != "sentinel.io/v1alpha1" || r.Kind != "Runbook" || r.Metadata.Name == "" {
		return fmt.Errorf("apiVersion, kind, and metadata.name are required")
	}
	switch r.Spec.Action.Type {
	case RecoveryRestartWorkload, RecoveryScaleWorkload, RecoveryRollbackRelease:
	default:
		return fmt.Errorf("unsupported action")
	}
	if r.Spec.Action.Type == RecoveryScaleWorkload && (r.Spec.Action.Parameters.MinReplicas < 1 || r.Spec.Action.Parameters.MaxReplicas < r.Spec.Action.Parameters.MinReplicas || r.Spec.Action.Parameters.Step < 1) {
		return fmt.Errorf("scale bounds are invalid")
	}
	return nil
}
func (svc *RecoveryService) Plan(ctx context.Context, r *http.Request, id string) (RecoveryPlan, error) {
	incident, err := svc.incidentService.Get(ctx, r, id)
	if err != nil {
		return RecoveryPlan{}, err
	}
	service, err := svc.services.find(incident.Service)
	if err != nil {
		return RecoveryPlan{}, err
	}
	books, err := svc.Load()
	if err != nil {
		return RecoveryPlan{}, err
	}
	book := svc.planner.Plan(*incident, service, books)
	plan := RecoveryPlan{IncidentID: id, Service: service.Metadata.Name, Status: RecoveryPlanNotActionable, BlockedReasons: []string{}}
	plan.Diagnosis.Category = "RUNTIME_FAILURE"
	plan.Diagnosis.Reason = incident.Runtime.Reason
	plan.Target = RecoveryTarget{service.Runtime.Namespace, service.Runtime.Workload.Kind, service.Runtime.Workload.Name}
	if book == nil {
		plan.Reason = "No safe recovery runbook matches this incident."
		return plan, nil
	}
	plan.Runbook = book
	plan.Action = book.Spec.Action
	plan.Risk = book.Spec.Risk.Level
	plan.ID = recoveryPlanID(id, book.Metadata.Name, plan.Target)
	plan.Approval = RemediationApproval{Required: book.Spec.Approval.Required, Approved: svc.approvedFor(plan)}
	plan.Policy = RemediationPolicy{Decision: firstNonEmpty(os.Getenv("S_SENTINEL_RECOVERY_POLICY_DECISION"), "REQUIRE_APPROVAL"), Reason: "recovery policy evaluation"}
	plan.Preflight = recoveryEligibility(plan)
	plan.BlockedReasons = plan.Preflight.BlockingReasons
	if plan.Preflight.Eligible {
		plan.Status = RecoveryPlanReady
	} else {
		plan.Status = RecoveryPlanBlocked
		plan.Reason = plan.Preflight.Reason
	}
	if execution := svc.executionFor(plan.ID); execution != nil {
		plan.Execution = execution
		v, _ := svc.verify(ctx, plan, *execution)
		plan.Verification = &v
	}
	return plan, nil
}
func recoveryPlanID(incident, book string, target RecoveryTarget) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(incident + ":" + book + ":" + target.Namespace + ":" + target.Kind + ":" + target.Name))
	return fmt.Sprintf("RP-%08x", h.Sum32())
}
func recoveryEligibility(plan RecoveryPlan) RemediationEligibility {
	blocked := []string{}
	if !strings.EqualFold(os.Getenv("S_SENTINEL_RECOVERY_ENABLED"), "true") {
		blocked = append(blocked, "S_SENTINEL_RECOVERY_ENABLED is not enabled")
	}
	if !strings.EqualFold(os.Getenv("S_SENTINEL_ALLOW_RECOVERY_"+string(plan.Action.Type)), "true") {
		blocked = append(blocked, "S_SENTINEL_ALLOW_RECOVERY_"+string(plan.Action.Type)+" is not enabled")
	}
	if policyDenied(plan.Policy.Decision) {
		blocked = append(blocked, "recovery policy denies this action")
	}
	if plan.Approval.Required && !plan.Approval.Approved {
		blocked = append(blocked, "human approval required")
	}
	if len(blocked) > 0 {
		return RemediationEligibility{Reason: blocked[0], BlockingReasons: blocked}
	}
	return RemediationEligibility{Eligible: true, BlockingReasons: []string{}}
}
func (svc *RecoveryService) approvedFor(plan RecoveryPlan) bool {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	state, ok := svc.approvals[plan.ID]
	return ok && state.Approved && state.PlanID == plan.ID && state.IncidentID == plan.IncidentID && state.Service == plan.Service && state.Action == plan.Action.Type && state.Target == plan.Target
}
func (svc *RecoveryService) Approve(ctx context.Context, r *http.Request, incidentID, planID string) (RecoveryPlan, error) {
	plan, err := svc.Plan(ctx, r, incidentID)
	if err != nil {
		return plan, err
	}
	if planID == "" || plan.ID == "" || planID != plan.ID {
		return plan, &RemediationRequestError{StatusCode: 400, Message: "planId does not match the current recovery plan"}
	}
	if plan.Runbook == nil || plan.Status == RecoveryPlanNotActionable {
		return plan, &RemediationRequestError{StatusCode: 409, Message: "no actionable recovery plan is available"}
	}
	svc.mu.Lock()
	svc.approvals[plan.ID] = RecoveryApprovalState{PlanID: plan.ID, IncidentID: plan.IncidentID, Service: plan.Service, Action: plan.Action.Type, Target: plan.Target, Approved: true, ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
	svc.mu.Unlock()
	return svc.Plan(ctx, r, incidentID)
}
func (svc *RecoveryService) Preview(ctx context.Context, r *http.Request, id string) (RecoveryPlan, error) {
	return svc.Plan(ctx, r, id)
}
func (svc *RecoveryService) Execute(ctx context.Context, r *http.Request, id, planID string) (RecoveryPlan, error) {
	plan, err := svc.Plan(ctx, r, id)
	if err != nil {
		return plan, err
	}
	if planID != plan.ID {
		return plan, &RemediationRequestError{StatusCode: 400, Message: "planId does not match the current recovery plan"}
	}
	if !plan.Preflight.Eligible {
		return plan, &RemediationRequestError{StatusCode: 409, Message: plan.Preflight.Reason}
	}
	if plan.Action.Type == RecoveryRollbackRelease {
		// Rollback is deliberately not implemented by RecoveryExecutor. It is a
		// narrow bridge to the Stage 6 remediation adapter and its established
		// Runtime Action result, policy, approval, and execution gates.
		legacy, legacyErr := svc.rollback.Execute(ctx, r, id, "ROLLBACK")
		if legacyErr != nil {
			return plan, legacyErr
		}
		x := RecoveryExecution{RequestKey: plan.ID, Action: RecoveryRollbackRelease, Status: "SUCCEEDED"}
		if legacy.Execution == nil || legacy.Execution.Status != "SUCCEEDED" {
			x.Status, x.Reason = "FAILED", "existing rollback pipeline did not verify the action"
		}
		svc.mu.Lock()
		svc.executions[plan.ID] = x
		svc.mu.Unlock()
		plan.Execution = &x
		verification, _ := svc.verify(ctx, plan, x)
		plan.Verification = &verification
		return plan, nil
	}
	if svc.executor == nil || !svc.executor.Supports(plan.Action.Type) {
		return plan, &RemediationRequestError{StatusCode: 409, Message: "recovery executor is unavailable"}
	}
	svc.mu.Lock()
	if x, ok := svc.executions[plan.ID]; ok {
		x.Idempotent = true
		plan.Execution = &x
		svc.mu.Unlock()
		return plan, nil
	}
	svc.mu.Unlock()
	if err := svc.executor.Preflight(ctx, plan); err != nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: err.Error()}
	}
	replicas, err := svc.executor.Execute(ctx, plan)
	x := RecoveryExecution{RequestKey: plan.ID, Action: plan.Action.Type, ExpectedReplicas: replicas, Status: "SUCCEEDED"}
	if err != nil {
		x.Status = "FAILED"
		x.Reason = err.Error()
	}
	svc.mu.Lock()
	svc.executions[plan.ID] = x
	svc.mu.Unlock()
	plan.Execution = &x
	v, _ := svc.verify(ctx, plan, x)
	plan.Verification = &v
	return plan, nil
}
func (svc *RecoveryService) Verification(ctx context.Context, r *http.Request, id string) (RecoveryVerification, error) {
	plan, err := svc.Plan(ctx, r, id)
	if err != nil {
		return RecoveryVerification{}, err
	}
	if plan.Execution == nil {
		return RecoveryVerification{Status: RecoveryVerificationPending, Reason: "no recovery execution has been recorded"}, nil
	}
	return svc.verify(ctx, plan, *plan.Execution)
}
func (svc *RecoveryService) verify(ctx context.Context, plan RecoveryPlan, x RecoveryExecution) (RecoveryVerification, error) {
	if x.Status != "SUCCEEDED" {
		return RecoveryVerification{Status: RecoveryVerificationFailed, Reason: x.Reason}, nil
	}
	snap, err := svc.runtimeService.Snapshot(ctx, plan.Service)
	if err != nil || snap.Status == RuntimeStatusUnknown {
		return RecoveryVerification{Status: RecoveryVerificationUnknown, Reason: "post-action runtime status is unavailable"}, nil
	}
	w := snap.Workload
	if plan.Action.Type == RecoveryScaleWorkload && w.DesiredReplicas != x.ExpectedReplicas {
		return RecoveryVerification{Status: RecoveryVerificationRecovering, RuntimeStatus: snap.Status, Reason: "desired replicas have not reached the bounded scale target"}, nil
	}
	if snap.Status == RuntimeStatusHealthy && w.DesiredReplicas > 0 && w.ReadyReplicas == w.DesiredReplicas && w.AvailableReplicas == w.DesiredReplicas {
		return RecoveryVerification{Status: RecoveryVerificationRecovered, RuntimeStatus: snap.Status, Reason: "runtime is healthy and all desired replicas are ready"}, nil
	}
	return RecoveryVerification{Status: RecoveryVerificationRecovering, RuntimeStatus: snap.Status, Reason: "runtime action completed while workloads are still reconciling"}, nil
}
func (svc *RecoveryService) executionFor(id string) *RecoveryExecution {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	x, ok := svc.executions[id]
	if !ok {
		return nil
	}
	return &x
}
