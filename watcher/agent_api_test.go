package main

import (
	"context"
	"errors"
	"testing"
)

func TestDeterministicAgentRuntimeFailureAndGuard(t *testing.T) {
	b := Runbook{}
	b.Metadata.Name = "restart-unhealthy-workload"
	b.Spec.Action.Type = RecoveryRestartWorkload
	c := ReliabilityAgentContext{Incident: ReliabilityIncident{ID: "INC-runtime"}, Runtime: RuntimeSnapshot{Status: RuntimeStatusUnhealthy, Workload: RuntimeWorkloadStatus{DesiredReplicas: 3}}, Runbooks: []Runbook{b}, Fingerprint: "f1"}
	d, e := DeterministicAgentProvider{}.Analyze(context.Background(), c)
	if e != nil || d.Category != AgentDiagnosisRuntimeFailure || len(d.CandidateRunbooks) != 1 {
		t.Fatalf("unexpected diagnosis %#v %v", d, e)
	}
	bad := AgentDiagnosis{Category: AgentDiagnosisRuntimeFailure, Summary: "x", Confidence: .9, CandidateRunbooks: []AgentRunbookCandidate{{ID: "delete-all-pods", Confidence: .9}}}
	if !validateAgentDiagnosis(&bad, c) || len(bad.CandidateRunbooks) != 0 {
		t.Fatalf("forbidden candidate was not filtered %#v", bad)
	}
}

type failingAgentProvider struct{}

func (failingAgentProvider) Analyze(context.Context, ReliabilityAgentContext) (AgentDiagnosis, error) {
	return AgentDiagnosis{}, errors.New("unavailable")
}
