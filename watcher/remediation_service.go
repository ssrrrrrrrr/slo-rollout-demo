package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const noActionableReleaseRemediation = "No actionable release remediation is available for this incident."

type RemediationService struct {
	incidentService  *IncidentService
	sloService       *SLOService
	runtimeService   *RuntimeService
	repository       EvidenceRepository
	executionAdapter RemediationExecutionAdapter
	operation        *OperationService
}

func NewRemediationService(incidentService *IncidentService, sloService *SLOService, runtimeService *RuntimeService, repository EvidenceRepository, executionAdapter RemediationExecutionAdapter) *RemediationService {
	return &RemediationService{incidentService: incidentService, sloService: sloService, runtimeService: runtimeService, repository: repository, executionAdapter: executionAdapter, operation: NewOperationService(NewOperationExecutorRegistry(ReleaseRuntimeActionExecutorAdapter{adapter: executionAdapter}))}
}

func (api *portalAPI) remediationService() *RemediationService {
	if api.remediationSvc != nil {
		return api.remediationSvc
	}
	svc := NewRemediationService(api.incidentService(), api.sloService(), api.runtimeService(), api.evidenceRepository(), NewRuntimeActionPipelineAdapter(api.cfg.RepoDir, api.reportDir))
	svc.operation = api.operationService()
	api.remediationSvc = svc
	return svc
}

func (svc *RemediationService) Plan(ctx context.Context, r *http.Request, incidentID string) (RemediationPlan, error) {
	incident, err := svc.incidentService.Get(ctx, r, incidentID)
	if err != nil {
		return RemediationPlan{}, err
	}
	plan := RemediationPlan{IncidentID: incident.ID, Service: incident.Service, Status: RemediationPlanNotActionable, RelatedRelease: incident.RelatedRelease, Recommendation: RemediationAction{Action: "NONE"}, AllowedActions: []string{}, Eligibility: RemediationEligibility{BlockingReasons: []string{}}}
	if incident.RelatedRelease == nil || incident.RelatedRelease.ID == "" {
		plan.Reason = noActionableReleaseRemediation
		plan.Eligibility.Reason = plan.Reason
		return plan, nil
	}

	release := &ServiceReleaseSummary{ID: incident.RelatedRelease.ID, Status: incident.RelatedRelease.Status, Timestamp: incident.RelatedRelease.Timestamp}
	plan.Target = RemediationTarget{ReleaseID: release.ID}
	if incident.ReleaseEvidence != nil {
		release.PolicyDecision, release.FinalAction = incident.ReleaseEvidence.PolicyDecision, incident.ReleaseEvidence.FinalAction
	}
	if incident.Recommendation != nil && release.FinalAction == "" {
		release.FinalAction = incident.Recommendation.Action
	}
	if !svc.incidentService.IsCurrentRelease(release) {
		plan.Reason = noActionableReleaseRemediation
		plan.Eligibility.Reason = "related release is stale or has no usable timestamp"
		return plan, nil
	}

	evidence := svc.releaseEvidence(r, release.ID)
	if evidence.PolicyDecision != "" {
		release.PolicyDecision = evidence.PolicyDecision
	}
	if evidence.Action != "" {
		release.FinalAction = evidence.Action
	}
	plan.Policy = RemediationPolicy{Decision: release.PolicyDecision, Reason: evidence.PolicyReason}
	plan.Approval = RemediationApproval{Required: evidence.ApprovalRequired || policyRequiresApproval(release.PolicyDecision), Approved: evidence.Approved}
	action := supportedRemediationAction(release.FinalAction)
	if action == "" {
		plan.Reason = noActionableReleaseRemediation
		plan.Eligibility.Reason = "no existing supported release recommendation is available"
		return plan, nil
	}
	plan.Status = RemediationPlanActionable
	plan.Recommendation = RemediationAction{Action: action, Source: evidence.ActionSource, Reason: evidence.ActionReason}
	plan.Operation = "Delegate the " + action + " recommendation through the existing Release Control execution flow for release " + release.ID
	if plan.Recommendation.Source == "" {
		if incident.Recommendation != nil && incident.Recommendation.Source != "" {
			plan.Recommendation.Source = incident.Recommendation.Source
		} else {
			plan.Recommendation.Source = "release evidence"
		}
	}
	plan.AllowedActions = []string{action}
	plan.OperationID = remediationControlledOperation(plan).ID
	plan.Eligibility = remediationEligibility(plan)
	if policyDenied(plan.Policy.Decision) {
		plan.Status = RemediationPlanBlocked
	}
	if plan.Status == RemediationPlanActionable && plan.Eligibility.Eligible {
		if svc.executionAdapter == nil {
			plan.Status = RemediationPlanBlocked
			plan.Eligibility = remediationBlockedEligibility(plan.Eligibility, "existing runtime action execution adapter is unavailable")
		} else if err := svc.executionAdapter.Available(RemediationExecutionRequest{ReleaseID: release.ID, Action: action}); err != nil {
			plan.Status = RemediationPlanBlocked
			plan.Eligibility = remediationBlockedEligibility(plan.Eligibility, err.Error())
		}
	}
	svc.projectDurableOperation(ctx, &plan)
	return plan, nil
}

func remediationBlockedEligibility(eligibility RemediationEligibility, reason string) RemediationEligibility {
	eligibility.Eligible = false
	eligibility.BlockingReasons = append(eligibility.BlockingReasons, reason)
	if eligibility.Reason == "" {
		eligibility.Reason = reason
	}
	return eligibility
}

type remediationEvidenceProjection struct {
	Action, ActionSource, ActionReason, PolicyDecision, PolicyReason string
	ApprovalRequired, Approved                                       bool
}

func (svc *RemediationService) releaseEvidence(r *http.Request, releaseID string) remediationEvidenceProjection {
	if svc.repository == nil {
		return remediationEvidenceProjection{}
	}
	response, err := svc.repository.GetRelease(r, EvidenceReleaseQuery{ReleaseID: releaseID, IncludeRaw: true})
	if err != nil || response == nil {
		return remediationEvidenceProjection{}
	}
	var document interface{}
	if json.Unmarshal(response.Body, &document) != nil {
		return remediationEvidenceProjection{}
	}
	projection := remediationEvidenceProjection{}
	collectRemediationEvidence(document, &projection)
	return projection
}

func collectRemediationEvidence(value interface{}, projection *remediationEvidenceProjection) {
	switch item := value.(type) {
	case map[string]interface{}:
		for key, raw := range item {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			if text, ok := raw.(string); ok {
				switch normalized {
				case "recommendedaction", "finalaction", "action":
					if projection.Action == "" {
						projection.Action, projection.ActionSource = text, "release advisor/evidence"
					}
				case "policydecision":
					if projection.PolicyDecision == "" {
						projection.PolicyDecision = text
					}
				case "reason":
					if projection.PolicyReason == "" {
						projection.PolicyReason = text
					}
				}
			}
			if flag, ok := raw.(bool); ok {
				switch normalized {
				case "requireshumanapproval", "approvalrequired":
					projection.ApprovalRequired = projection.ApprovalRequired || flag
				case "approved", "approvalapproved":
					projection.Approved = projection.Approved || flag
				}
			}
			collectRemediationEvidence(raw, projection)
		}
	case []interface{}:
		for _, item := range item {
			collectRemediationEvidence(item, projection)
		}
	}
}

func remediationEligibility(plan RemediationPlan) RemediationEligibility {
	blocking := []string{}
	if strings.TrimSpace(plan.Policy.Decision) == "" {
		blocking = append(blocking, "release policy decision is unavailable")
	}
	if policyDenied(plan.Policy.Decision) {
		blocking = append(blocking, "release policy decision denies this action")
	}
	if plan.Approval.Required && !plan.Approval.Approved {
		blocking = append(blocking, "human approval required")
	}
	blocking = append(blocking, runtimeGateBlockingReasons(plan.Recommendation.Action)...)
	eligibility := RemediationEligibility{Eligible: len(blocking) == 0, BlockingReasons: blocking}
	if len(blocking) > 0 {
		eligibility.Reason = blocking[0]
	}
	return eligibility
}

func runtimeGateBlockingReasons(action string) []string {
	checks := []struct{ name, value string }{
		{"S_SENTINEL_RUNTIME_EXECUTION_ENABLED", os.Getenv("S_SENTINEL_RUNTIME_EXECUTION_ENABLED")},
		{"S_SENTINEL_RUNTIME_ACTION_APPROVED", os.Getenv("S_SENTINEL_RUNTIME_ACTION_APPROVED")},
		{"S_SENTINEL_ALLOW_RUNTIME_" + action, os.Getenv("S_SENTINEL_ALLOW_RUNTIME_" + action)},
		{"S_SENTINEL_RUNTIME_" + action + "_EXECUTE", os.Getenv("S_SENTINEL_RUNTIME_" + action + "_EXECUTE")},
	}
	blocking := []string{}
	for _, check := range checks {
		if !strings.EqualFold(strings.TrimSpace(check.value), "true") {
			blocking = append(blocking, check.name+" is not enabled")
		}
	}
	return blocking
}

func (svc *RemediationService) Preview(ctx context.Context, r *http.Request, incidentID string) (RemediationPlan, error) {
	plan, err := svc.Plan(ctx, r, incidentID)
	if err != nil {
		return RemediationPlan{}, err
	}
	// Preview intentionally projects the existing release plan and gates only;
	// it never invokes the execution delegate.
	return plan, nil
}

func (svc *RemediationService) Execute(ctx context.Context, r *http.Request, incidentID, requestedAction string) (RemediationPlan, error) {
	plan, err := svc.Plan(ctx, r, incidentID)
	if err != nil {
		return RemediationPlan{}, err
	}
	requestedAction = supportedRemediationAction(requestedAction)
	if requestedAction == "" || len(plan.AllowedActions) != 1 || requestedAction != plan.AllowedActions[0] {
		return plan, &RemediationRequestError{StatusCode: 400, Message: "requested action is not allowed by the remediation plan"}
	}
	if !plan.Eligibility.Eligible {
		return plan, &RemediationRequestError{StatusCode: 409, Message: plan.Eligibility.Reason}
	}
	if svc.operation == nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: "controlled operation service is unavailable"}
	}
	op := remediationControlledOperation(plan)
	plan.OperationID = op.ID
	previous, _ := svc.operation.Get(ctx, op.ID)
	if previous != nil && (previous.ExecutionIntent.Action != op.Action || previous.ExecutionIntent.ReleaseID != plan.Target.ReleaseID || previous.ExecutionIntent.Target.ReleaseID != plan.Target.ReleaseID) {
		return plan, &RemediationRequestError{StatusCode: 409, Message: "durable release operation intent no longer matches the current remediation target"}
	}
	result, err := svc.operation.Execute(ctx, op)
	var ledgerErr *OperationLedgerError
	if errors.As(err, &ledgerErr) {
		return plan, &RemediationRequestError{StatusCode: 409, Message: ledgerErr.Error()}
	}
	execution := RemediationExecution{RequestKey: op.IdempotencyKey, Action: requestedAction, ExecutedAt: time.Now().Format(time.RFC3339), Target: plan.Target}
	if err != nil {
		execution.Status, execution.Reason = "FAILED", err.Error()
	} else {
		execution.Status = result.Execution.Status
		execution.StartedAt, execution.FinishedAt = result.Execution.StartedAt, result.Execution.FinishedAt
		execution.Reason, execution.PostState = result.Execution.Reason, result.PostState
		execution.ResultID, execution.ActionVerified = result.Execution.ExternalResultID, result.ActionVerified
		if result.ExternalTarget.Namespace != "" || result.ExternalTarget.WorkloadName != "" {
			execution.Target = RemediationTarget{ReleaseID: result.ExternalTarget.ReleaseID, Namespace: result.ExternalTarget.Namespace, Workload: result.ExternalTarget.WorkloadName}
		}
		if (execution.Status == "SUCCEEDED" && !execution.ActionVerified) || (execution.Status != "SUCCEEDED" && execution.Status != "UNKNOWN") {
			execution.Status = "FAILED"
		}
	}
	if previous != nil && previous.State != OperationStatePlanned && previous.State != OperationStateWaitingApproval {
		execution.Idempotent = true
	}
	plan.Execution = &execution
	if execution.Status == "UNKNOWN" {
		plan.Verification = &RemediationVerification{Status: RemediationVerificationUnknown, Reason: firstNonEmpty(execution.Reason, "durable operation execution state is unknown")}
		return plan, nil
	}
	verification, _ := svc.verify(ctx, r, plan, execution)
	plan.Verification = &verification
	_, _ = svc.operation.RecordVerification(ctx, op.ID, operationVerificationFromRemediation(verification))
	svc.incidentService.RecordRemediationVerification(ctx, incidentID, verification)
	return plan, nil
}

func (svc *RemediationService) Verification(ctx context.Context, r *http.Request, incidentID string) (RemediationVerification, error) {
	plan, err := svc.Plan(ctx, r, incidentID)
	if err != nil {
		return RemediationVerification{}, err
	}
	if plan.Execution == nil {
		return RemediationVerification{Status: RemediationVerificationPending, Reason: "no remediation execution has been recorded"}, nil
	}
	verification, err := svc.verify(ctx, r, plan, *plan.Execution)
	if err == nil && plan.OperationID != "" {
		_, _ = svc.operation.RecordVerification(ctx, plan.OperationID, operationVerificationFromRemediation(verification))
	}
	return verification, err
}

func (svc *RemediationService) verify(ctx context.Context, _ *http.Request, plan RemediationPlan, execution RemediationExecution) (RemediationVerification, error) {
	if execution.Status != "SUCCEEDED" {
		return RemediationVerification{Status: RemediationVerificationFailed, Reason: execution.Reason}, nil
	}
	runtime, runtimeErr := svc.runtimeService.Snapshot(ctx, plan.Service)
	slo, sloErr := svc.sloService.Evaluate(ctx, plan.Service)
	if runtimeErr != nil || sloErr != nil {
		return RemediationVerification{Status: RemediationVerificationUnknown, ActionVerified: true, Reason: "post-action Service verification is unavailable"}, nil
	}
	verification := RemediationVerification{ActionVerified: true, RuntimeStatus: runtime.Status, RuntimeRecovered: runtime.Status == RuntimeStatusHealthy, SLOStatus: slo.Status, SLORecovered: slo.Status != SLOStatusBreached, BurnRate1h: slo.BurnRate.OneHour}
	switch {
	case runtime.Status == RuntimeStatusUnknown || slo.Status == SLOStatusUnknown:
		verification.Status, verification.Reason = RemediationVerificationUnknown, "post-action runtime or SLO state is unknown"
	case runtime.Status == RuntimeStatusHealthy && slo.Status != SLOStatusBreached:
		verification.Status, verification.Reason = RemediationVerificationRecovered, "runtime is healthy and SLO is no longer breached"
	case runtime.Status == RuntimeStatusHealthy:
		verification.Status, verification.Reason = RemediationVerificationRecovering, "runtime is healthy while the long-window SLO remains breached or at risk"
	default:
		verification.Status, verification.Reason = RemediationVerificationFailed, "runtime has not recovered"
	}
	return verification, nil
}

func (svc *RemediationService) projectDurableOperation(ctx context.Context, plan *RemediationPlan) {
	if svc.operation == nil || plan.OperationID == "" {
		return
	}
	op, err := svc.operation.Get(ctx, plan.OperationID)
	if err != nil {
		return
	}
	if op.Execution.Status != "" {
		plan.Execution = &RemediationExecution{RequestKey: op.ID, Action: plan.Recommendation.Action, ExecutedAt: op.Execution.FinishedAt, StartedAt: op.Execution.StartedAt, FinishedAt: op.Execution.FinishedAt, Status: op.Execution.Status, Reason: op.Execution.Reason, Target: plan.Target, PostState: op.Execution.PostState, ResultID: op.Execution.ExternalResultID, ActionVerified: op.Execution.ActionVerified, Idempotent: true}
	}
	if op.Verification.Status != "" {
		plan.Verification = remediationVerificationFromOperation(op.Verification)
	}
}

func operationVerificationFromRemediation(v RemediationVerification) OperationVerificationState {
	return OperationVerificationState{Status: string(v.Status), Reason: v.Reason, RuntimeStatus: v.RuntimeStatus, SLOStatus: v.SLOStatus, ActionVerified: v.ActionVerified}
}

func remediationVerificationFromOperation(v OperationVerificationState) *RemediationVerification {
	return &RemediationVerification{Status: RemediationVerificationStatus(v.Status), Reason: v.Reason, RuntimeStatus: v.RuntimeStatus, SLOStatus: v.SLOStatus, ActionVerified: v.ActionVerified, RuntimeRecovered: v.Status == "RECOVERED", SLORecovered: v.Status == "RECOVERED" || v.SLOStatus != SLOStatusBreached}
}

func remediationRequestKey(incidentID, releaseID, action string) string {
	return incidentID + ":" + releaseID + ":" + action
}
func supportedRemediationAction(action string) string {
	action = strings.ToUpper(strings.TrimSpace(action))
	switch action {
	case "PAUSE", "PAUSE_ROLLOUT":
		return "PAUSE"
	case "RESUME", "RESUME_ROLLOUT":
		return "RESUME"
	case "PROMOTE", "PROMOTE_ROLLOUT":
		return "PROMOTE"
	case "ABORT", "ABORT_ROLLOUT":
		return "ABORT"
	case "ROLLBACK", "ROLLBACK_ROLLOUT":
		return "ROLLBACK"
	}
	return ""
}
func policyDenied(decision string) bool { return strings.Contains(strings.ToUpper(decision), "DENY") }
func policyRequiresApproval(decision string) bool {
	return strings.Contains(strings.ToUpper(decision), "APPROVAL")
}

type RemediationRequestError struct {
	StatusCode int
	Message    string
}

func (err *RemediationRequestError) Error() string {
	return fmt.Sprintf("remediation request: %s", err.Message)
}
