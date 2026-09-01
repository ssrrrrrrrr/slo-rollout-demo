package main

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"
)

// IncidentLifecycleService translates current detector observations into
// durable episodes. It is intentionally invoked by requests today; a future
// controller only needs to call ReconcileService and adds no new logic.
type IncidentLifecycleService struct {
	incidents  *IncidentService
	repository IncidentRepository
	now        func() time.Time
}

func NewIncidentLifecycleService(incidents *IncidentService, repository IncidentRepository) *IncidentLifecycleService {
	return &IncidentLifecycleService{incidents: incidents, repository: repository, now: time.Now}
}

func (s *IncidentLifecycleService) ReconcileService(ctx context.Context, request *http.Request, serviceName string) (*ReliabilityIncident, error) {
	observation, err := s.incidents.observe(ctx, request, serviceName)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	if observation.Candidate == nil {
		active, err := s.repository.FindActiveByService(ctx, serviceName)
		if err != nil {
			return nil, err
		}
		if !observation.ProvidersKnown {
			return firstIncident(active), nil
		}
		for _, incident := range active {
			if !s.canResolve(incident, observation) {
				continue
			}
			if err := s.transition(ctx, &incident, IncidentStatusResolved, "INCIDENT_RESOLVED", "Recovery conditions are explicitly satisfied", nil); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	candidate := observation.Candidate
	if candidate.Fingerprint == "" {
		candidate.Fingerprint = deterministicIncidentFingerprint(candidate.Service + ":" + candidate.PrimarySignal.Type)
	}
	existing, err := s.repository.FindActiveByFingerprint(ctx, candidate.Fingerprint)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		// Signal growth can change detector primary-signal priority. Keep the
		// existing service episode instead of creating an artificial duplicate.
		active, listErr := s.repository.FindActiveByService(ctx, serviceName)
		if listErr != nil {
			return nil, listErr
		}
		if len(active) == 1 {
			existing = &active[0]
		}
	}
	if existing == nil {
		incident := durableIncidentFromObservation(*candidate, newDurableIncidentID(candidate.Fingerprint, s.now()), now)
		if err := s.repository.Create(ctx, incident); err != nil {
			return nil, err
		}
		if err := s.repository.AppendEvent(ctx, incident.ID, IncidentTimelineEvent{Type: "INCIDENT_DETECTED", Message: "Incident detected", OccurredAt: now, Payload: map[string]interface{}{"fingerprint": incident.Fingerprint, "primarySignal": incident.PrimarySignal.Type}}); err != nil {
			return nil, err
		}
		return s.withTimeline(ctx, &incident)
	}
	updated := mergeObservation(*existing, *candidate, now)
	reactivated := existing.Status == IncidentStatusRecovering
	if reactivated {
		updated.Status = IncidentStatusActive
		updated.RecoveringAt = existing.RecoveringAt
	}
	if err := s.repository.Update(ctx, updated); err != nil {
		return nil, err
	}
	if !sameSignals(existing.Signals, updated.Signals) {
		if err := s.repository.AppendEvent(ctx, updated.ID, IncidentTimelineEvent{Type: "SIGNALS_CHANGED", Message: "Incident signals changed", OccurredAt: now, Payload: map[string]interface{}{"signals": updated.Signals}}); err != nil {
			return nil, err
		}
	}
	if existing.Severity != updated.Severity {
		if err := s.repository.AppendEvent(ctx, updated.ID, IncidentTimelineEvent{Type: "SEVERITY_CHANGED", Message: fmt.Sprintf("Severity changed %s → %s", existing.Severity, updated.Severity), OccurredAt: now, Payload: map[string]interface{}{"from": existing.Severity, "to": updated.Severity}}); err != nil {
			return nil, err
		}
	}
	if reactivated {
		if err := s.repository.AppendEvent(ctx, updated.ID, IncidentTimelineEvent{Type: "INCIDENT_REACTIVATED", Message: "Reliability failure recurred while recovering", OccurredAt: now}); err != nil {
			return nil, err
		}
	}
	return s.withTimeline(ctx, &updated)
}

func (s *IncidentLifecycleService) OperationStarted(ctx context.Context, operation ControlledOperation) {
	if operation.Source.Type != "INCIDENT" {
		return
	}
	incident, err := s.repository.Get(ctx, operation.Source.ID)
	if err != nil {
		return
	}
	if incident.Status == IncidentStatusResolved {
		return
	}
	_ = s.transition(ctx, incident, IncidentStatusMitigating, "OPERATION_EXECUTION_STARTED", "Controlled operation execution started", operationEventPayload(operation))
}
func (s *IncidentLifecycleService) OperationCompleted(ctx context.Context, operation ControlledOperation, result OperationExecutionResult, err error) {
	if operation.Source.Type != "INCIDENT" {
		return
	}
	incident, getErr := s.repository.Get(ctx, operation.Source.ID)
	if getErr != nil || incident.Status == IncidentStatusResolved {
		return
	}
	eventType, message := "OPERATION_EXECUTION_SUCCEEDED", "Controlled operation execution succeeded"
	if err != nil || result.Execution.Status != "SUCCEEDED" {
		eventType, message = "OPERATION_EXECUTION_FAILED", "Controlled operation execution failed"
	}
	_ = s.append(ctx, incident, IncidentTimelineEvent{Type: eventType, Message: message, OccurredAt: s.now().UTC().Format(time.RFC3339), Payload: operationEventPayload(operation)})
}
func (s *IncidentLifecycleService) OperationBlocked(ctx context.Context, operation ControlledOperation, reason string) {
	if operation.Source.Type != "INCIDENT" {
		return
	}
	incident, err := s.repository.Get(ctx, operation.Source.ID)
	if err != nil || incident.Status == IncidentStatusResolved {
		return
	}
	payload := operationEventPayload(operation)
	payload["reason"] = reason
	_ = s.append(ctx, incident, IncidentTimelineEvent{Type: "OPERATION_BLOCKED", Message: "Controlled operation is blocked", OccurredAt: s.now().UTC().Format(time.RFC3339), Payload: payload})
}
func (s *IncidentLifecycleService) RecoveryApproved(ctx context.Context, incidentID string, plan RecoveryPlan) {
	incident, err := s.repository.Get(ctx, incidentID)
	if err != nil {
		return
	}
	_ = s.append(ctx, incident, IncidentTimelineEvent{Type: "RECOVERY_APPROVED", Message: "Recovery plan approved", OccurredAt: s.now().UTC().Format(time.RFC3339), Payload: map[string]interface{}{"operationId": plan.OperationID, "action": plan.Action.Type, "target": plan.Target}})
}
func (s *IncidentLifecycleService) AgentAnalysisCompleted(ctx context.Context, incidentID string, diagnosis AgentDiagnosis) {
	incident, err := s.repository.Get(ctx, incidentID)
	if err != nil {
		return
	}
	ids := make([]string, 0, len(diagnosis.CandidateRunbooks))
	for _, candidate := range diagnosis.CandidateRunbooks {
		ids = append(ids, candidate.ID)
	}
	_ = s.append(ctx, incident, IncidentTimelineEvent{Type: "AGENT_ANALYSIS_COMPLETED", Message: "Reliability agent analysis completed", OccurredAt: s.now().UTC().Format(time.RFC3339), Payload: map[string]interface{}{"category": diagnosis.Category, "summary": diagnosis.Summary, "confidence": diagnosis.Confidence, "candidateRunbookIds": ids, "provider": diagnosis.Provider, "fallbackUsed": diagnosis.FallbackUsed}})
}
func (s *IncidentLifecycleService) RecoveryVerification(ctx context.Context, incidentID string, verification RecoveryVerification) {
	incident, err := s.repository.Get(ctx, incidentID)
	if err != nil || incident.Status == IncidentStatusResolved {
		return
	}
	switch verification.Status {
	case RecoveryVerificationRecovering:
		_ = s.transition(ctx, incident, IncidentStatusRecovering, "RECOVERY_VERIFICATION_RECOVERING", verification.Reason, nil)
	case RecoveryVerificationRecovered:
		_ = s.transition(ctx, incident, IncidentStatusResolved, "RECOVERY_VERIFICATION_RECOVERED", verification.Reason, nil)
	case RecoveryVerificationFailed:
		_ = s.append(ctx, incident, IncidentTimelineEvent{Type: "VERIFICATION_FAILED", Message: verification.Reason, OccurredAt: s.now().UTC().Format(time.RFC3339), Payload: nil})
	}
}
func (s *IncidentLifecycleService) RemediationVerification(ctx context.Context, incidentID string, verification RemediationVerification) {
	incident, err := s.repository.Get(ctx, incidentID)
	if err != nil || incident.Status == IncidentStatusResolved {
		return
	}
	switch verification.Status {
	case RemediationVerificationRecovering:
		_ = s.transition(ctx, incident, IncidentStatusRecovering, "REMEDIATION_VERIFICATION_RECOVERING", verification.Reason, nil)
	case RemediationVerificationRecovered:
		_ = s.transition(ctx, incident, IncidentStatusResolved, "REMEDIATION_VERIFICATION_RECOVERED", verification.Reason, nil)
	case RemediationVerificationFailed:
		_ = s.append(ctx, incident, IncidentTimelineEvent{Type: "VERIFICATION_FAILED", Message: verification.Reason, OccurredAt: s.now().UTC().Format(time.RFC3339)})
	}
}

func (s *IncidentLifecycleService) transition(ctx context.Context, incident *ReliabilityIncident, next IncidentStatus, eventType, message string, payload map[string]interface{}) error {
	if incident.Status == IncidentStatusResolved || incident.Status == next {
		return nil
	}
	if !validIncidentTransition(incident.Status, next) {
		return nil
	}
	now := s.now().UTC().Format(time.RFC3339)
	incident.Status = next
	incident.UpdatedAt = now
	switch next {
	case IncidentStatusMitigating:
		incident.MitigationStartedAt = now
	case IncidentStatusRecovering:
		incident.RecoveringAt = now
	case IncidentStatusResolved:
		incident.ResolvedAt = now
	}
	if err := s.repository.Update(ctx, *incident); err != nil {
		return err
	}
	return s.repository.AppendEvent(ctx, incident.ID, IncidentTimelineEvent{Type: eventType, Message: message, OccurredAt: now, Payload: payload})
}
func (s *IncidentLifecycleService) canResolve(incident ReliabilityIncident, observation currentIncidentObservation) bool {
	if observation.SLO.Status == SLOStatusUnknown || observation.Runtime.Status == RuntimeStatusUnknown {
		return false
	}
	for _, signal := range incident.Signals {
		switch signal.Type {
		case "RUNTIME_UNHEALTHY", "RUNTIME_DEGRADED":
			if observation.Runtime.Status != RuntimeStatusHealthy {
				return false
			}
		case "SLO_BREACH":
			if observation.SLO.Status == SLOStatusBreached {
				return false
			}
		default:
			if len(signal.Type) >= len("RELEASE_") && signal.Type[:len("RELEASE_")] == "RELEASE_" && s.incidents.IsCurrentReleaseFailure(observation.LatestRelease) {
				return false
			}
		}
	}
	return true
}
func (s *IncidentLifecycleService) append(ctx context.Context, incident *ReliabilityIncident, event IncidentTimelineEvent) error {
	return s.repository.AppendEvent(ctx, incident.ID, event)
}
func (s *IncidentLifecycleService) withTimeline(ctx context.Context, incident *ReliabilityIncident) (*ReliabilityIncident, error) {
	events, err := s.repository.ListEvents(ctx, incident.ID)
	if err != nil {
		return nil, err
	}
	copy := *incident
	copy.Timeline = events
	return &copy, nil
}
func durableIncidentFromObservation(observation ReliabilityIncident, id, now string) ReliabilityIncident {
	observation.ID = id
	observation.Status = IncidentStatusActive
	observation.FirstObservedAt = now
	observation.LastObservedAt = now
	observation.StartedAt = now
	observation.ObservedAt = now
	observation.CreatedAt = now
	observation.UpdatedAt = now
	observation.Timeline = nil
	return observation
}
func mergeObservation(existing, observation ReliabilityIncident, now string) ReliabilityIncident {
	observation.ID = existing.ID
	observation.Fingerprint = existing.Fingerprint
	observation.Status = existing.Status
	observation.FirstObservedAt = existing.FirstObservedAt
	observation.LastObservedAt = now
	observation.StartedAt = existing.StartedAt
	observation.ObservedAt = now
	observation.CreatedAt = existing.CreatedAt
	observation.UpdatedAt = now
	observation.MitigationStartedAt = existing.MitigationStartedAt
	observation.RecoveringAt = existing.RecoveringAt
	observation.ResolvedAt = existing.ResolvedAt
	observation.Timeline = nil
	return observation
}
func validIncidentTransition(from, next IncidentStatus) bool {
	switch from {
	case IncidentStatusActive:
		return next == IncidentStatusMitigating || next == IncidentStatusRecovering || next == IncidentStatusResolved
	case IncidentStatusMitigating:
		return next == IncidentStatusRecovering || next == IncidentStatusResolved
	case IncidentStatusRecovering:
		return next == IncidentStatusActive || next == IncidentStatusResolved
	}
	return false
}
func sameSignals(a, b []IncidentSignal) bool { return reflect.DeepEqual(a, b) }
func firstIncident(items []ReliabilityIncident) *ReliabilityIncident {
	if len(items) == 0 {
		return nil
	}
	return &items[0]
}
func operationEventPayload(operation ControlledOperation) map[string]interface{} {
	return map[string]interface{}{"operationId": operation.ID, "action": operation.Action, "target": operation.Target}
}
