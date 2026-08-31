package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const noActionableReleaseRemediation = "No actionable release remediation is available for this incident."

type RemediationService struct {
	incidentService  *IncidentService
	sloService       *SLOService
	runtimeService   *RuntimeService
	repository       EvidenceRepository
	executionAdapter RemediationExecutionAdapter
	mu               sync.Mutex
	executions       map[string]RemediationExecution
}

func NewRemediationService(incidentService *IncidentService, sloService *SLOService, runtimeService *RuntimeService, repository EvidenceRepository, executionAdapter RemediationExecutionAdapter) *RemediationService {
	return &RemediationService{incidentService: incidentService, sloService: sloService, runtimeService: runtimeService, repository: repository, executionAdapter: executionAdapter, executions: map[string]RemediationExecution{}}
}

func (api *portalAPI) remediationService() *RemediationService {
	if api.remediationSvc != nil {
		return api.remediationSvc
	}
	return NewRemediationService(api.incidentService(), api.sloService(), api.runtimeService(), api.evidenceRepository(), NewRuntimeActionPipelineAdapter(api.cfg.RepoDir, api.reportDir))
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
	plan.Execution = svc.executionFor(remediationRequestKey(plan.IncidentID, release.ID, action))
	if plan.Execution != nil {
		verification, _ := svc.verify(ctx, r, plan, *plan.Execution)
		plan.Verification = &verification
	}
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
	if svc.executionAdapter == nil {
		return plan, &RemediationRequestError{StatusCode: 409, Message: "existing runtime action execution adapter is unavailable"}
	}
	key := remediationRequestKey(plan.IncidentID, plan.RelatedRelease.ID, requestedAction)
	svc.mu.Lock()
	if existing, ok := svc.executions[key]; ok {
		existing.Idempotent = true
		plan.Execution = &existing
		svc.mu.Unlock()
		return plan, nil
	}
	svc.executions[key] = RemediationExecution{RequestKey: key, Status: "EXECUTING", Action: requestedAction, Target: plan.Target}
	svc.mu.Unlock()
	result, err := svc.executionAdapter.Execute(ctx, RemediationExecutionRequest{ReleaseID: plan.RelatedRelease.ID, Action: requestedAction})
	if unavailable := (*RuntimeActionPipelineUnavailableError)(nil); errors.As(err, &unavailable) {
		svc.mu.Lock()
		delete(svc.executions, key)
		svc.mu.Unlock()
		return plan, &RemediationRequestError{StatusCode: 409, Message: unavailable.Error()}
	}
	execution := RemediationExecution{RequestKey: key, Action: requestedAction, ExecutedAt: time.Now().Format(time.RFC3339), Target: plan.Target}
	if err != nil {
		execution.Status, execution.Reason = "FAILED", err.Error()
	} else {
		execution.Status = result.Status
		execution.StartedAt, execution.FinishedAt = result.StartedAt, result.FinishedAt
		execution.Reason, execution.Target, execution.PostState = result.Reason, result.Target, result.PostState
		execution.ResultID, execution.ActionVerified = result.ResultID, result.ActionVerified
		if execution.Status != "SUCCEEDED" || !execution.ActionVerified {
			execution.Status = "FAILED"
		}
	}
	svc.mu.Lock()
	svc.executions[key] = execution
	svc.mu.Unlock()
	plan.Execution = &execution
	verification, _ := svc.verify(ctx, r, plan, execution)
	plan.Verification = &verification
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
	return svc.verify(ctx, r, plan, *plan.Execution)
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

func (svc *RemediationService) executionFor(key string) *RemediationExecution {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	result, ok := svc.executions[key]
	if !ok {
		return nil
	}
	return &result
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
