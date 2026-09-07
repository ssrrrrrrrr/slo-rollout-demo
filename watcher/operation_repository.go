package main

import (
	"context"
	"fmt"
	"time"
)

// OperationRepository is deliberately independent from EvidenceRepository.
// Operations are durable control-plane records, not Release artifacts.
type OperationRepository interface {
	Create(context.Context, ControlledOperation) error
	Update(context.Context, ControlledOperation) error
	Get(context.Context, string) (*ControlledOperation, error)
	List(context.Context, OperationListQuery) ([]ControlledOperation, error)
	FindByIncident(context.Context, string) ([]ControlledOperation, error)
	AppendEvent(context.Context, string, OperationTimelineEvent) error
	ListEvents(context.Context, string) ([]OperationTimelineEvent, error)
	Close() error
}

type OperationListQuery struct {
	IncidentID string
	States     []OperationLifecycleState
}

type OperationTimelineEvent struct {
	ID          string                 `json:"id"`
	OperationID string                 `json:"operationId"`
	Type        string                 `json:"type"`
	Timestamp   time.Time              `json:"timestamp"`
	Summary     string                 `json:"summary"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
}

type OperationNotFoundError struct{ ID string }

func (e *OperationNotFoundError) Error() string {
	if e.ID == "" {
		return "operation not found"
	}
	return fmt.Sprintf("operation %s not found", e.ID)
}
