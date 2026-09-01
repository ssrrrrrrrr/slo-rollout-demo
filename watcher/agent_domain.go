package main

import (
	"context"
	"time"
)

type AgentDiagnosisCategory string

const (
	AgentDiagnosisReleaseFailure AgentDiagnosisCategory = "RELEASE_FAILURE"
	AgentDiagnosisRuntimeFailure AgentDiagnosisCategory = "RUNTIME_FAILURE"
	AgentDiagnosisSLODegradation AgentDiagnosisCategory = "SLO_DEGRADATION"
	AgentDiagnosisCapacityRisk   AgentDiagnosisCategory = "CAPACITY_RISK"
	AgentDiagnosisMultiSignal    AgentDiagnosisCategory = "MULTI_SIGNAL"
	AgentDiagnosisUnknown        AgentDiagnosisCategory = "UNKNOWN"
)

type AgentRunbookCandidate struct {
	ID         string  `json:"id"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}
type AgentDiagnosis struct {
	AnalysisID         string                  `json:"analysisId"`
	IncidentID         string                  `json:"incidentId"`
	Category           AgentDiagnosisCategory  `json:"category"`
	Summary            string                  `json:"summary"`
	Evidence           []string                `json:"evidence"`
	Confidence         float64                 `json:"confidence"`
	CandidateRunbooks  []AgentRunbookCandidate `json:"candidateRunbooks"`
	ReasoningSummary   string                  `json:"reasoningSummary"`
	Provider           string                  `json:"provider"`
	Model              string                  `json:"model,omitempty"`
	StartedAt          string                  `json:"startedAt"`
	FinishedAt         string                  `json:"finishedAt"`
	FallbackUsed       bool                    `json:"fallbackUsed"`
	ContextFingerprint string                  `json:"contextFingerprint"`
}
type ReliabilityAgentContext struct {
	Service         Service                  `json:"service"`
	SLO             ServiceSLOStatus         `json:"slo"`
	Runtime         RuntimeSnapshot          `json:"runtime"`
	Incident        ReliabilityIncident      `json:"incident"`
	LatestRelease   *ServiceReleaseSummary   `json:"latestRelease,omitempty"`
	ReleaseEvidence *IncidentReleaseEvidence `json:"releaseEvidence,omitempty"`
	Runbooks        []Runbook                `json:"runbooks"`
	Fingerprint     string                   `json:"fingerprint"`
}
type ReliabilityAgentProvider interface {
	Analyze(context.Context, ReliabilityAgentContext) (AgentDiagnosis, error)
}
type agentAnalysisCacheEntry struct {
	fingerprint string
	diagnosis   AgentDiagnosis
	createdAt   time.Time
}
