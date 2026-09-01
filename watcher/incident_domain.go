package main

import "time"

type IncidentStatus string

const (
	IncidentStatusActive     IncidentStatus = "ACTIVE"
	IncidentStatusMitigating IncidentStatus = "MITIGATING"
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

// ReliabilityIncident is the durable incident episode projection returned by
// the API. ReliabilityIncidentDetector still produces an observation/candidate;
// IncidentLifecycleService assigns the durable ID and lifecycle fields.
type ReliabilityIncident struct {
	ID                  string                   `json:"id"`
	Fingerprint         string                   `json:"fingerprint,omitempty"`
	Service             string                   `json:"service"`
	Status              IncidentStatus           `json:"status"`
	Severity            IncidentSeverity         `json:"severity"`
	Title               string                   `json:"title"`
	PrimarySignal       IncidentSignal           `json:"primarySignal"`
	Signals             []IncidentSignal         `json:"signals"`
	RelatedRelease      *IncidentCorrelation     `json:"relatedRelease,omitempty"`
	Recommendation      *IncidentRecommendation  `json:"recommendation,omitempty"`
	ReleaseEvidence     *IncidentReleaseEvidence `json:"releaseEvidence,omitempty"`
	SLO                 ServiceSLOStatus         `json:"slo"`
	Runtime             RuntimeSnapshot          `json:"runtime"`
	Timeline            []IncidentTimelineEvent  `json:"timeline"`
	StartedAt           string                   `json:"startedAt"`
	ObservedAt          string                   `json:"observedAt"`
	FirstObservedAt     string                   `json:"firstObservedAt,omitempty"`
	LastObservedAt      string                   `json:"lastObservedAt,omitempty"`
	MitigationStartedAt string                   `json:"mitigationStartedAt,omitempty"`
	RecoveringAt        string                   `json:"recoveringAt,omitempty"`
	ResolvedAt          string                   `json:"resolvedAt,omitempty"`
	CreatedAt           string                   `json:"createdAt,omitempty"`
	UpdatedAt           string                   `json:"updatedAt,omitempty"`
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
	ID         string                 `json:"id,omitempty"`
	Type       string                 `json:"type"`
	Message    string                 `json:"message"`
	OccurredAt string                 `json:"occurredAt"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
}

func incidentObservedAt() string {
	return time.Now().Format(time.RFC3339)
}
