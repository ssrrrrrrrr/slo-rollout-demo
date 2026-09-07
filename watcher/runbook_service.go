package main

import (
	"context"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"hash/fnv"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	operation       *OperationService
	agent           *ReliabilityAgentService
}

func NewRecoveryService(incidents *IncidentService, runtime *RuntimeService, services *ServiceService, repoDir string, executor RecoveryExecutor) *RecoveryService {
	rollback := NewRemediationService(incidents, incidents.sloService, runtime, services.repository, NewRuntimeActionPipelineAdapter(repoDir, filepath.Join(repoDir, "docs", "release-reports")))
	operation := NewOperationService(NewOperationExecutorRegistry(
		ReleaseRuntimeActionExecutorAdapter{adapter: rollback.executionAdapter},
		KubernetesRecoveryExecutorAdapter{executor: executor},
	))
	rollback.operation = operation
	return &RecoveryService{incidentService: incidents, runtimeService: runtime, services: services, runbookDir: filepath.Join(repoDir, "configs", "runbooks"), planner: DeterministicRecoveryPlanner{}, executor: executor, rollback: rollback, operation: operation}
}
func (api *portalAPI) recoveryService() *RecoveryService {
	if api.recoverySvc != nil {
		return api.recoverySvc
	}
	svc := NewRecoveryService(api.incidentService(), api.runtimeService(), api.serviceService(), api.cfg.RepoDir, NewKubernetesRecoveryExecutor())
	svc.operation = api.operationService()
	svc.rollback.operation = svc.operation
	api.recoverySvc = svc
	return svc
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
	if incident.RelatedRelease != nil {
		plan.ReleaseID = incident.RelatedRelease.ID
	}
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
	plan.Approval = RemediationApproval{Required: book.Spec.Approval.Required}
	plan.Policy = RemediationPolicy{Decision: firstNonEmpty(os.Getenv("S_SENTINEL_RECOVERY_POLICY_DECISION"), "REQUIRE_APPROVAL"), Reason: "recovery policy evaluation"}
	plan.Preflight = recoveryEligibility(plan)
	plan.BlockedReasons = plan.Preflight.BlockingReasons
	if plan.Preflight.Eligible {
		plan.Status = RecoveryPlanReady
	} else {
		plan.Status = RecoveryPlanBlocked
		plan.Reason = plan.Preflight.Reason
	}
	plan.OperationID = recoveryControlledOperation(plan, nil, nil).ID
	svc.projectDurableOperation(ctx, &plan)
	plan.Preflight = recoveryEligibility(plan)
	plan.BlockedReasons = plan.Preflight.BlockingReasons
	if plan.Preflight.Eligible {
		plan.Status = RecoveryPlanReady
	} else {
		plan.Status = RecoveryPlanBlocked
		plan.Reason = plan.Preflight.Reason
	}
	plan.PlannerSource = "DETERMINISTIC"
	if svc.agent != nil {
		if analysis := svc.agent.Cached(ctx, r, id); analysis != nil {
			plan.AgentAnalysis = analysis
			for _, candidate := range analysis.CandidateRunbooks {
				if candidate.ID == book.Metadata.Name {
					plan.PlannerSource = "AGENT_ASSISTED"
					break
				}
			}
		}
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
	return false
}
func (svc *RecoveryService) approvalFor(plan RecoveryPlan) *RecoveryApprovalState {
	if svc.operation == nil || plan.OperationID == "" {
		return nil
	}
	op, err := svc.operation.Get(context.Background(), plan.OperationID)
	if err != nil || !op.Approval.Approved {
		return nil
	}
	return &RecoveryApprovalState{PlanID: plan.ID, IncidentID: plan.IncidentID, Service: plan.Service, Action: plan.Action.Type, Target: plan.Target, Approved: true, ApprovedAt: recoveryApprovalTime(op.Approval)}
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
	op, err := svc.prepareOperationIntent(ctx, plan, nil)
	if err != nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: err.Error()}
	}
	if _, err := svc.operation.ApproveRecovery(ctx, op, plan.ID); err != nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: err.Error()}
	}
	approved, err := svc.Plan(ctx, r, incidentID)
	if err == nil {
		svc.incidentService.RecordRecoveryApproval(ctx, incidentID, approved)
	}
	return approved, err
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
	if svc.operation == nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: "controlled operation service is unavailable"}
	}
	var releasePlan *RemediationPlan
	if plan.Action.Type == RecoveryRollbackRelease {
		candidate, candidateErr := svc.rollback.Plan(ctx, r, id)
		if candidateErr != nil {
			return plan, candidateErr
		}
		if candidate.Recommendation.Action != "ROLLBACK" || !candidate.Eligibility.Eligible {
			return plan, &RemediationRequestError{StatusCode: 409, Message: firstNonEmpty(candidate.Eligibility.Reason, "existing release rollback plan is not eligible")}
		}
		releasePlan = &candidate
	}
	op, err := svc.prepareOperationIntent(ctx, plan, releasePlan)
	if err != nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: err.Error()}
	}
	plan.OperationID = op.ID
	previous, _ := svc.operation.Get(ctx, op.ID)
	result, err := svc.operation.Execute(ctx, op)
	var ledgerErr *OperationLedgerError
	if errors.As(err, &ledgerErr) {
		return plan, &RemediationRequestError{StatusCode: 409, Message: ledgerErr.Error()}
	}
	x := RecoveryExecution{RequestKey: op.IdempotencyKey, Action: plan.Action.Type, ExpectedReplicas: result.ExpectedReplicas, Status: result.Execution.Status, Reason: result.Execution.Reason}
	if err != nil {
		x.Status = "FAILED"
		x.Reason = err.Error()
	}
	if previous != nil && previous.State != OperationStatePlanned && previous.State != OperationStateWaitingApproval {
		x.Idempotent = true
	}
	plan.Execution = &x
	if x.Status == "UNKNOWN" {
		plan.Verification = &RecoveryVerification{Status: RecoveryVerificationUnknown, Reason: firstNonEmpty(x.Reason, "durable operation execution state is unknown")}
		return plan, nil
	}
	v, _ := svc.verify(ctx, plan, x)
	plan.Verification = &v
	_, _ = svc.operation.RecordVerification(ctx, op.ID, operationVerificationFromRecovery(v))
	svc.incidentService.RecordRecoveryVerification(ctx, id, v)
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
	verification, err := svc.verify(ctx, plan, *plan.Execution)
	if err == nil && plan.OperationID != "" {
		_, _ = svc.operation.RecordVerification(ctx, plan.OperationID, operationVerificationFromRecovery(verification))
	}
	return verification, err
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
func (svc *RecoveryService) projectDurableOperation(ctx context.Context, plan *RecoveryPlan) {
	if svc.operation == nil || plan.OperationID == "" {
		return
	}
	op, err := svc.operation.Get(ctx, plan.OperationID)
	if err != nil {
		return
	}
	plan.Approval.Approved = op.Approval.Approved
	if op.Execution.Status != "" {
		plan.Execution = &RecoveryExecution{RequestKey: op.ID, Action: plan.Action.Type, ExpectedReplicas: op.Execution.ExpectedReplicas, Status: op.Execution.Status, Reason: op.Execution.Reason}
	}
	if op.Verification.Status != "" {
		plan.Verification = recoveryVerificationFromOperation(op.Verification)
	}
}

func (svc *RecoveryService) prepareOperationIntent(ctx context.Context, plan RecoveryPlan, releasePlan *RemediationPlan) (ControlledOperation, error) {
	op := recoveryControlledOperation(plan, svc.approvalFor(plan), releasePlan)
	if existing, err := svc.operation.Get(ctx, op.ID); err == nil {
		if err := svc.operationIntentStillValid(ctx, plan, *existing); err != nil {
			return ControlledOperation{}, err
		}
		// Keep the current policy/preflight/approval projection fresh while
		// taking the immutable intent exclusively from the durable record.
		op.ExecutionIntent = existing.ExecutionIntent
		return op, nil
	}
	if op.Action != OperationScaleWorkload {
		return op, nil
	}
	snapshot, err := svc.runtimeService.Snapshot(ctx, plan.Service)
	if err != nil || snapshot.Status == RuntimeStatusUnknown {
		return ControlledOperation{}, fmt.Errorf("scale operation intent cannot be frozen because runtime state is unavailable")
	}
	current := snapshot.Workload.DesiredReplicas
	target := boundedScaleTarget(current, plan.Action.Parameters)
	if target == current {
		return ControlledOperation{}, fmt.Errorf("scale target is already within its configured bound")
	}
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, InitialReplicas: &current, TargetReplicas: &target}
	return op, nil
}

// operationIntentStillValid is a fresh execution gate: it verifies current
// service semantics without ever replacing the durable intent with a newly
// calculated value.
func (svc *RecoveryService) operationIntentStillValid(ctx context.Context, plan RecoveryPlan, operation ControlledOperation) error {
	intent := operation.ExecutionIntent
	if intent.Action != operation.Action || intent.Target.Namespace != plan.Target.Namespace || intent.Target.WorkloadKind != plan.Target.Kind || intent.Target.WorkloadName != plan.Target.Name {
		return fmt.Errorf("durable operation intent no longer matches the current service runtime target")
	}
	if operation.Action == OperationRollbackRelease && intent.ReleaseID != plan.ReleaseID {
		return fmt.Errorf("durable release operation intent no longer matches the current related release")
	}
	if operation.Action != OperationScaleWorkload {
		return nil
	}
	if intent.InitialReplicas == nil || intent.TargetReplicas == nil {
		return fmt.Errorf("durable scale operation intent is incomplete")
	}
	if *intent.TargetReplicas < plan.Action.Parameters.MinReplicas || *intent.TargetReplicas > plan.Action.Parameters.MaxReplicas {
		return fmt.Errorf("durable scale target is outside the current safety bounds")
	}
	snapshot, err := svc.runtimeService.Snapshot(ctx, plan.Service)
	if err != nil || snapshot.Status == RuntimeStatusUnknown {
		return fmt.Errorf("scale operation intent cannot be validated because runtime state is unavailable")
	}
	if snapshot.Workload.DesiredReplicas != *intent.InitialReplicas {
		return fmt.Errorf("durable scale operation intent is stale because current replicas changed")
	}
	return nil
}

func recoveryApprovalTime(approval OperationApprovalState) string {
	for _, check := range approval.RequiredChecks {
		if check.Type == "RECOVERY_APPROVAL" && check.Approved {
			return check.ApprovedAt
		}
	}
	return ""
}

func operationVerificationFromRecovery(v RecoveryVerification) OperationVerificationState {
	return OperationVerificationState{Status: string(v.Status), Reason: v.Reason, RuntimeStatus: v.RuntimeStatus}
}

func recoveryVerificationFromOperation(v OperationVerificationState) *RecoveryVerification {
	return &RecoveryVerification{Status: RecoveryVerificationStatus(v.Status), RuntimeStatus: v.RuntimeStatus, Reason: v.Reason}
}
