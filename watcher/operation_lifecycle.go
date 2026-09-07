package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// OperationLifecycleService is the only component that advances a durable
// operation state. It deliberately has no Execute method: startup recovery
// observes external state and never retries a mutation automatically.
type OperationLifecycleService struct {
	repository OperationRepository
	inspector  OperationExecutionInspector
	now        func() time.Time
}

func NewOperationLifecycleService(repository OperationRepository, inspector OperationExecutionInspector) *OperationLifecycleService {
	return &OperationLifecycleService{repository: repository, inspector: inspector, now: time.Now}
}

// Create materializes immutable intent before a durable operation can be made
// READY or EXECUTING. Reloading the same deterministic operation returns the
// original record and never regenerates intent.
func (s *OperationLifecycleService) Create(ctx context.Context, operation ControlledOperation) (*ControlledOperation, error) {
	if err := validateOperationIdentity(operation); err != nil {
		return nil, err
	}
	if existing, err := s.repository.Get(ctx, operation.ID); err == nil {
		if !sameOperationDefinition(*existing, operation) {
			return nil, fmt.Errorf("operation ID %s already exists with a different immutable definition", operation.ID)
		}
		return existing, nil
	} else if !isOperationNotFound(err) {
		return nil, err
	}
	now := s.now().UTC()
	if operation.CreatedAt.IsZero() {
		operation.CreatedAt = now
	}
	operation.UpdatedAt = now
	if operation.State == "" {
		operation.State = OperationStatePlanned
	}
	intent, err := materializeOperationIntent(operation, now)
	if err != nil {
		return nil, err
	}
	operation.ExecutionIntent = intent
	if err := s.repository.Create(ctx, operation); err != nil {
		return nil, err
	}
	if err := s.appendEvent(ctx, operation, "OPERATION_CREATED", "Operation ledger record created", nil); err != nil {
		return nil, err
	}
	return &operation, nil
}

// Transition advances a durable state according to the centralized lifecycle
// graph. A no-op transition intentionally emits no duplicate timeline event.
func (s *OperationLifecycleService) Transition(ctx context.Context, operationID string, next OperationLifecycleState, eventType, summary string, payload map[string]interface{}) (*ControlledOperation, error) {
	operation, err := s.repository.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if operation.State == next {
		return operation, nil
	}
	if !operationTransitionAllowed(operation.State, next) {
		return nil, fmt.Errorf("operation transition %s -> %s is not allowed", operation.State, next)
	}
	now := s.now().UTC()
	operation.State, operation.UpdatedAt = next, now
	if next == OperationStateExecuting && operation.StartedAt == nil {
		operation.StartedAt = &now
	}
	if operationFinishesAt(next) && operation.FinishedAt == nil {
		operation.FinishedAt = &now
	}
	if err := s.repository.Update(ctx, *operation); err != nil {
		return nil, err
	}
	if eventType != "" {
		if err := s.appendEvent(ctx, *operation, eventType, summary, payload); err != nil {
			return nil, err
		}
	}
	return operation, nil
}

// ReconcileInFlight only reconciles states that may have been interrupted by
// a process crash. It never executes a mutation and never handles approvals
// or planning state.
func (s *OperationLifecycleService) ReconcileInFlight(ctx context.Context, operationID string) (*ControlledOperation, error) {
	operation, err := s.repository.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}
	switch operation.State {
	case OperationStateSucceeded, OperationStateVerifying, OperationStateRecovering:
		return operation, nil
	case OperationStateExecuting:
		// continue below
	default:
		return operation, nil
	}
	if s.inspector == nil {
		return s.markUnknown(ctx, *operation, "external execution state cannot be determined: inspector is unavailable", "")
	}
	inspection, inspectErr := s.inspector.Inspect(ctx, *operation)
	if inspectErr != nil {
		return s.markUnknown(ctx, *operation, "external execution state cannot be determined: "+inspectErr.Error(), "")
	}
	switch inspection.Status {
	case OperationInspectionApplied:
		operation, err = s.Transition(ctx, operation.ID, OperationStateSucceeded, "EXECUTION_EFFECT_OBSERVED", firstNonEmpty(inspection.Reason, "execution intent is observed externally"), operationInspectionPayload(inspection))
		if err != nil {
			return nil, err
		}
		return operation, nil
	case OperationInspectionNotApplied:
		if err := s.appendEvent(ctx, *operation, "EXECUTION_EFFECT_NOT_OBSERVED", firstNonEmpty(inspection.Reason, "execution intent is not observed externally"), operationInspectionPayload(inspection)); err != nil {
			return nil, err
		}
		return s.markUnknown(ctx, *operation, "execution effect was not observed; automatic retry is disabled", inspection.ExternalReference)
	default:
		return s.markUnknown(ctx, *operation, firstNonEmpty(inspection.Reason, "external execution state cannot be determined"), inspection.ExternalReference)
	}
}

func (s *OperationLifecycleService) markUnknown(ctx context.Context, operation ControlledOperation, reason, externalReference string) (*ControlledOperation, error) {
	payload := map[string]interface{}{}
	if externalReference != "" {
		payload["externalReference"] = externalReference
	}
	return s.Transition(ctx, operation.ID, OperationStateUnknown, "EXECUTION_STATE_UNKNOWN", reason, payload)
}

func (s *OperationLifecycleService) appendEvent(ctx context.Context, operation ControlledOperation, eventType, summary string, payload map[string]interface{}) error {
	return s.repository.AppendEvent(ctx, operation.ID, OperationTimelineEvent{OperationID: operation.ID, Type: eventType, Timestamp: s.now().UTC(), Summary: summary, Payload: payload})
}

func validateOperationIdentity(operation ControlledOperation) error {
	if operation.ID == "" || operation.Source.ID == "" || operation.Subject.ID == "" || operation.Action == "" {
		return fmt.Errorf("operation ID, source, subject, and action are required")
	}
	if expected := BuildOperationID(operation.Source, operation.Subject, operation.Action, operation.Target); expected != operation.ID {
		return fmt.Errorf("operation ID does not match its deterministic identity")
	}
	return nil
}

func materializeOperationIntent(operation ControlledOperation, now time.Time) (OperationExecutionIntent, error) {
	intent := operation.ExecutionIntent
	if intent.Action == "" {
		intent.Action = operation.Action
	}
	if intent.Action != operation.Action {
		return OperationExecutionIntent{}, fmt.Errorf("execution intent action does not match operation action")
	}
	if reflect.DeepEqual(intent.Target, OperationTarget{}) {
		intent.Target = operation.Target
	}
	if !reflect.DeepEqual(intent.Target, operation.Target) {
		return OperationExecutionIntent{}, fmt.Errorf("execution intent target does not match operation target")
	}
	if intent.ReleaseID == "" {
		intent.ReleaseID = operation.Target.ReleaseID
	}
	if intent.ReleaseID != operation.Target.ReleaseID {
		return OperationExecutionIntent{}, fmt.Errorf("execution intent release does not match operation target")
	}
	if operation.Action == OperationRollbackRelease && intent.RuntimeActionIdentity == "" {
		// The canonical operation ID is also the existing Runtime Action identity;
		// no secondary release-action identity is generated for the ledger.
		intent.RuntimeActionIdentity = operation.ID
	}
	switch operation.Action {
	case OperationRestartWorkload:
		if intent.RestartAt == "" {
			intent.RestartAt = now.Format(time.RFC3339Nano)
		}
	case OperationScaleWorkload:
		if intent.TargetReplicas == nil {
			return OperationExecutionIntent{}, fmt.Errorf("scale operation execution intent requires fixed targetReplicas")
		}
	}
	return intent, nil
}

func sameOperationDefinition(existing, requested ControlledOperation) bool {
	return existing.ID == requested.ID && existing.Source == requested.Source && existing.Subject == requested.Subject && existing.Action == requested.Action && reflect.DeepEqual(existing.Target, requested.Target) && (reflect.DeepEqual(requested.ExecutionIntent, OperationExecutionIntent{}) || reflect.DeepEqual(existing.ExecutionIntent, requested.ExecutionIntent))
}

func isOperationNotFound(err error) bool {
	var notFound *OperationNotFoundError
	return errors.As(err, &notFound)
}

func operationTransitionAllowed(from, to OperationLifecycleState) bool {
	allowed := map[OperationLifecycleState]map[OperationLifecycleState]bool{
		OperationStatePlanned:         {OperationStateWaitingApproval: true, OperationStateReady: true, OperationStateBlocked: true},
		OperationStateWaitingApproval: {OperationStateReady: true, OperationStateBlocked: true},
		OperationStateReady:           {OperationStateExecuting: true, OperationStateBlocked: true},
		OperationStateExecuting:       {OperationStateSucceeded: true, OperationStateFailed: true, OperationStateUnknown: true},
		OperationStateSucceeded:       {OperationStateVerifying: true},
		OperationStateVerifying:       {OperationStateRecovering: true, OperationStateRecovered: true, OperationStateFailed: true, OperationStateUnknown: true},
		OperationStateRecovering:      {OperationStateRecovered: true, OperationStateFailed: true},
	}
	return allowed[from][to]
}

func operationFinishesAt(state OperationLifecycleState) bool {
	return state == OperationStateSucceeded || state == OperationStateFailed || state == OperationStateRecovered || state == OperationStateBlocked || state == OperationStateUnknown
}

func operationInspectionPayload(result OperationInspectionResult) map[string]interface{} {
	if result.ExternalReference == "" {
		return nil
	}
	return map[string]interface{}{"externalReference": result.ExternalReference}
}
