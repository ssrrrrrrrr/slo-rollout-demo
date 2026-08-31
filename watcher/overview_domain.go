package main

type ReliabilityOverallStatus string

const (
	ReliabilityOverallHealthy   ReliabilityOverallStatus = "HEALTHY"
	ReliabilityOverallAtRisk    ReliabilityOverallStatus = "AT_RISK"
	ReliabilityOverallUnhealthy ReliabilityOverallStatus = "UNHEALTHY"
	ReliabilityOverallUnknown   ReliabilityOverallStatus = "UNKNOWN"
)

type ReliabilityOverview struct {
	SchemaVersion string                      `json:"schemaVersion"`
	GeneratedAt   string                      `json:"generatedAt"`
	Summary       FleetSummary                `json:"summary"`
	Services      []ServiceReliabilitySummary `json:"services"`
	Attention     []AttentionItem             `json:"attention"`
}

type FleetSummary struct {
	TotalServices     int `json:"totalServices"`
	HealthyServices   int `json:"healthyServices"`
	AtRiskServices    int `json:"atRiskServices"`
	UnhealthyServices int `json:"unhealthyServices"`
	UnknownServices   int `json:"unknownServices"`
	ActiveIncidents   int `json:"activeIncidents"`
	SEV1Incidents     int `json:"sev1Incidents"`
	SEV2Incidents     int `json:"sev2Incidents"`
	SEV3Incidents     int `json:"sev3Incidents"`
	SLOBreaches       int `json:"sloBreaches"`
	RuntimeUnhealthy  int `json:"runtimeUnhealthy"`
	RuntimeDegraded   int `json:"runtimeDegraded"`
	ReleaseRisks      int `json:"releaseRisks"`
}

type ServiceReliabilitySummary struct {
	Name                 string                   `json:"name"`
	DisplayName          string                   `json:"displayName"`
	OverallStatus        ReliabilityOverallStatus `json:"overallStatus"`
	SLOStatus            SLOStatus                `json:"sloStatus"`
	RuntimeStatus        RuntimeStatus            `json:"runtimeStatus"`
	IncidentStatus       IncidentStatus           `json:"incidentStatus,omitempty"`
	IncidentSeverity     IncidentSeverity         `json:"incidentSeverity,omitempty"`
	IncidentID           string                   `json:"incidentId,omitempty"`
	LatestRelease        *ServiceReleaseSummary   `json:"latestRelease"`
	ErrorBudgetRemaining *float64                 `json:"errorBudgetRemaining,omitempty"`
	BurnRate1h           *float64                 `json:"burnRate1h,omitempty"`
	ObservedAt           string                   `json:"observedAt"`
}

type AttentionItem struct {
	Service         string               `json:"service"`
	Priority        string               `json:"priority"`
	Type            string               `json:"type"`
	Title           string               `json:"title"`
	Reason          string               `json:"reason"`
	RelatedIncident string               `json:"relatedIncident,omitempty"`
	RelatedRelease  *IncidentCorrelation `json:"relatedRelease,omitempty"`
	ActionTarget    string               `json:"actionTarget"`
}
