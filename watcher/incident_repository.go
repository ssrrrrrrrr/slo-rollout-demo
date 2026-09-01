package main

import "context"

type IncidentListQuery struct {
	Service         string
	States          []IncidentStatus
	IncludeResolved bool
}

// IncidentRepository is intentionally independent from EvidenceRepository.
// Incidents are lifecycle records, not Release artifacts.
type IncidentRepository interface {
	Create(context.Context, ReliabilityIncident) error
	Update(context.Context, ReliabilityIncident) error
	Get(context.Context, string) (*ReliabilityIncident, error)
	List(context.Context, IncidentListQuery) ([]ReliabilityIncident, error)
	FindActiveByService(context.Context, string) ([]ReliabilityIncident, error)
	FindActiveByFingerprint(context.Context, string) (*ReliabilityIncident, error)
	AppendEvent(context.Context, string, IncidentTimelineEvent) error
	ListEvents(context.Context, string) ([]IncidentTimelineEvent, error)
	Close() error
}
