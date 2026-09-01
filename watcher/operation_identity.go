package main

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

func BuildOperationID(source OperationSource, subject OperationSubject, action OperationAction, target OperationTarget) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(source.Type + ":" + source.ID + "|" + subject.Type + ":" + subject.ID + "|" + string(action) + "|" + target.ReleaseID + "|" + target.Service + "|" + target.Namespace + "|" + target.WorkloadKind + "|" + target.WorkloadName))
	return fmt.Sprintf("OP-%08x", h.Sum32())
}

func remediationOperationAction(action string) OperationAction {
	switch supportedRemediationAction(action) {
	case "PAUSE":
		return OperationPauseRelease
	case "RESUME":
		return OperationResumeRelease
	case "PROMOTE":
		return OperationPromoteRelease
	case "ABORT":
		return OperationAbortRelease
	case "ROLLBACK":
		return OperationRollbackRelease
	}
	return ""
}

func recoveryOperationAction(action RecoveryActionType) OperationAction {
	switch action {
	case RecoveryRestartWorkload:
		return OperationRestartWorkload
	case RecoveryScaleWorkload:
		return OperationScaleWorkload
	case RecoveryRollbackRelease:
		return OperationRollbackRelease
	}
	return ""
}

func remediationControlledOperation(plan RemediationPlan) ControlledOperation {
	target := OperationTarget{ReleaseID: plan.Target.ReleaseID, Namespace: plan.Target.Namespace, WorkloadName: plan.Target.Workload, Service: plan.Service}
	source := OperationSource{Type: "INCIDENT", ID: plan.IncidentID}
	subject := OperationSubject{Type: "RELEASE", ID: plan.Target.ReleaseID}
	op := ControlledOperation{
		Subject: subject, Source: source, Action: remediationOperationAction(plan.Recommendation.Action), Target: target,
		Policy:    operationPolicy(plan.Policy, "release-evidence"),
		Preflight: operationPreflight(plan.Eligibility),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	op.ID = BuildOperationID(source, subject, op.Action, target)
	op.IdempotencyKey = op.ID
	op.Approval = operationApproval(op.ID, []OperationApprovalCheck{{Type: "RELEASE_APPROVAL", SubjectID: plan.Target.ReleaseID, Required: plan.Approval.Required, Approved: plan.Approval.Approved}})
	return op
}

func recoveryControlledOperation(plan RecoveryPlan, approval *RecoveryApprovalState, releasePlan *RemediationPlan) ControlledOperation {
	target := OperationTarget{Service: plan.Service, Namespace: plan.Target.Namespace, WorkloadKind: plan.Target.Kind, WorkloadName: plan.Target.Name, ReleaseID: plan.ReleaseID}
	subject := OperationSubject{Type: "SERVICE", ID: plan.Service}
	// The runbook remains the RecoveryPlan projection; the controlled action is
	// causally owned by its incident so lifecycle events can be recorded.
	source := OperationSource{Type: "INCIDENT", ID: plan.IncidentID}
	if plan.Action.Type == RecoveryRollbackRelease && plan.ReleaseID != "" {
		subject = OperationSubject{Type: "RELEASE", ID: plan.ReleaseID}
		// Release rollback has one physical Runtime Action target. Keep its
		// canonical identity independent of whether it was reached through a
		// release remediation or a matching recovery runbook.
		target = OperationTarget{Service: plan.Service, ReleaseID: plan.ReleaseID}
		source = OperationSource{Type: "INCIDENT", ID: plan.IncidentID}
	}
	policy := operationPolicy(plan.Policy, "recovery-policy")
	preflight := operationPreflight(plan.Preflight)
	checks := []OperationApprovalCheck{{Type: "RECOVERY_APPROVAL", SubjectID: plan.ID, Required: plan.Approval.Required, Approved: plan.Approval.Approved}}
	if approval != nil {
		checks[0].ApprovedAt, checks[0].ApprovedBy = approval.ApprovedAt, approval.ApprovedBy
	}
	if releasePlan != nil {
		target = OperationTarget{Service: plan.Service, ReleaseID: releasePlan.Target.ReleaseID}
		subject = OperationSubject{Type: "RELEASE", ID: releasePlan.Target.ReleaseID}
		source = OperationSource{Type: "INCIDENT", ID: plan.IncidentID}
		checks = append(checks, OperationApprovalCheck{Type: "RELEASE_APPROVAL", SubjectID: releasePlan.Target.ReleaseID, Required: releasePlan.Approval.Required, Approved: releasePlan.Approval.Approved})
		policy = operationPolicy(releasePlan.Policy, "release-evidence")
		preflight = mergeOperationPreflight(preflight, operationPreflight(releasePlan.Eligibility))
	}
	op := ControlledOperation{Subject: subject, Source: source, Action: recoveryOperationAction(plan.Action.Type), Target: target, Parameters: plan.Action.Parameters, Risk: plan.Risk, Policy: policy, Preflight: preflight, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	op.ID = BuildOperationID(source, subject, op.Action, target)
	op.IdempotencyKey = op.ID
	op.Approval = operationApproval(op.ID, checks)
	return op
}

func operationPolicy(policy RemediationPolicy, source string) OperationPolicyState {
	decision := strings.ToUpper(strings.TrimSpace(policy.Decision))
	switch {
	case strings.Contains(decision, "DENY"):
		decision = "DENY"
	case strings.Contains(decision, "APPROVAL"):
		decision = "REQUIRE_APPROVAL"
	case decision != "":
		decision = "ALLOW"
	default:
		decision = "UNKNOWN"
	}
	return OperationPolicyState{Decision: decision, Reason: policy.Reason, Source: source}
}

func operationApproval(operationID string, checks []OperationApprovalCheck) OperationApprovalState {
	result := OperationApprovalState{SubjectType: "OPERATION", SubjectID: operationID, RequiredChecks: checks, Approved: true}
	for _, check := range checks {
		if check.Required {
			result.Required = true
			result.Approved = result.Approved && check.Approved
		}
	}
	return result
}

func operationPreflight(eligibility RemediationEligibility) OperationPreflightState {
	state := OperationPreflightState{Checks: []string{}, BlockedReasons: append([]string{}, eligibility.BlockingReasons...)}
	if eligibility.Eligible {
		state.Status = "READY"
	} else if len(state.BlockedReasons) > 0 {
		state.Status = "BLOCKED"
	} else {
		state.Status = "UNKNOWN"
	}
	return state
}

func mergeOperationPreflight(left, right OperationPreflightState) OperationPreflightState {
	result := OperationPreflightState{Checks: append(append([]string{}, left.Checks...), right.Checks...), BlockedReasons: append(append([]string{}, left.BlockedReasons...), right.BlockedReasons...)}
	if left.Status == "READY" && right.Status == "READY" {
		result.Status = "READY"
	} else if len(result.BlockedReasons) > 0 {
		result.Status = "BLOCKED"
	} else {
		result.Status = "UNKNOWN"
	}
	return result
}
