package main

import "time"

type IncidentStatus string

const (
	IncidentStatusActive     IncidentStatus = "ACTIVE"
	IncidentStatusRecovering IncidentStatus = "RECOVERING"
	IncidentStatusResolved   IncidentStatus = "RESOLVED"
	IncidentStatusUnknown    IncidentStatus = "UNKNOWN"
)

type IncidentSeverity string

const (
	IncidentSeveritySEV1 IncidentSeverity = "SEV1"
	IncidentSeveritySEV2 IncidentSeverity = "SEV2"
	IncidentSeveritySEV3 IncidentSeverity = "SEV3"
	IncidentSeveritySEV4 IncidentSeverity = "SEV4"
)

// ReliabilityIncident is an ephemeral, explainable aggregation of current
// Service reliability signals. It does not represent a persisted incident record.
type ReliabilityIncident struct {
	ID              string                   `json:"id"`
	Service         string                   `json:"service"`
	Status          IncidentStatus           `json:"status"`
	Severity        IncidentSeverity         `json:"severity"`
	Title           string                   `json:"title"`
	PrimarySignal   IncidentSignal           `json:"primarySignal"`
	Signals         []IncidentSignal         `json:"signals"`
	RelatedRelease  *IncidentCorrelation     `json:"relatedRelease,omitempty"`
	Recommendation  *IncidentRecommendation  `json:"recommendation,omitempty"`
	ReleaseEvidence *IncidentReleaseEvidence `json:"releaseEvidence,omitempty"`
	SLO             ServiceSLOStatus         `json:"slo"`
	Runtime         RuntimeSnapshot          `json:"runtime"`
	Timeline        []IncidentTimelineEvent  `json:"timeline"`
	StartedAt       string                   `json:"startedAt"`
	ObservedAt      string                   `json:"observedAt"`
}

type IncidentSignal struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// IncidentCorrelation is explicitly temporal only. It never claims release causality.
type IncidentCorrelation struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Correlation string `json:"correlation"`
	Timestamp   string `json:"timestamp,omitempty"`
}

type IncidentRecommendation struct {
	Action string `json:"action"`
	Source string `json:"source"`
}

// IncidentReleaseEvidence mirrors existing release evidence without creating a
// second policy or execution domain for Incidents.
type IncidentReleaseEvidence struct {
	PolicyDecision string `json:"policyDecision,omitempty"`
	FinalAction    string `json:"finalAction,omitempty"`
}

type IncidentTimelineEvent struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	OccurredAt string `json:"occurredAt"`
}

func incidentObservedAt() string {
	return time.Now().Format(time.RFC3339)
}
