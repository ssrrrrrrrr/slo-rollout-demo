package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type DeterministicAgentProvider struct{}

func (DeterministicAgentProvider) Analyze(_ context.Context, c ReliabilityAgentContext) (AgentDiagnosis, error) {
	d := AgentDiagnosis{IncidentID: c.Incident.ID, Evidence: []string{}, CandidateRunbooks: []AgentRunbookCandidate{}, Confidence: .8, Provider: "deterministic", ContextFingerprint: c.Fingerprint}
	d.Evidence = []string{"runtimeStatus=" + string(c.Runtime.Status), fmt.Sprintf("readyReplicas=%d", c.Runtime.Workload.ReadyReplicas), fmt.Sprintf("desiredReplicas=%d", c.Runtime.Workload.DesiredReplicas)}
	switch {
	case c.Runtime.Status == RuntimeStatusUnhealthy:
		d.Category = AgentDiagnosisRuntimeFailure
		d.Summary = "Configured workload replicas are unavailable."
		d.Confidence = .92
		for _, b := range c.Runbooks {
			if b.Metadata.Name == "restart-unhealthy-workload" {
				d.CandidateRunbooks = []AgentRunbookCandidate{{ID: b.Metadata.Name, Confidence: .91, Reason: "runtime is unhealthy and the registered restart runbook is applicable"}}
			}
		}
	case c.SLO.Status == SLOStatusBreached && c.Runtime.Status == RuntimeStatusUnhealthy:
		d.Category = AgentDiagnosisMultiSignal
		d.Summary = "SLO breach and runtime failure are both present."
	case c.SLO.Status == SLOStatusBreached:
		d.Category = AgentDiagnosisSLODegradation
		d.Summary = "Service SLO is breached while runtime evidence is not unhealthy."
	case c.LatestRelease != nil && strings.Contains(strings.ToUpper(c.LatestRelease.Status), "FAIL"):
		d.Category = AgentDiagnosisReleaseFailure
		d.Summary = "A related failed release is present."
	default:
		d.Category = AgentDiagnosisUnknown
		d.Summary = "Available evidence does not support a more specific diagnosis."
	}
	d.ReasoningSummary = "Diagnosis is derived from current Service reliability signals."
	return d, nil
}

type OllamaReliabilityAgentProvider struct {
	URL, Model string
	Client     *http.Client
}

func (p OllamaReliabilityAgentProvider) Analyze(ctx context.Context, c ReliabilityAgentContext) (AgentDiagnosis, error) {
	payload := map[string]interface{}{"model": p.Model, "stream": false, "format": "json", "prompt": agentPrompt(c)}
	b, _ := json.Marshal(payload)
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.URL, "/")+"/api/generate", bytes.NewReader(b))
	if err != nil {
		return AgentDiagnosis{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return AgentDiagnosis{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return AgentDiagnosis{}, fmt.Errorf("ollama status %s", resp.Status)
	}
	var outer struct {
		Response string `json:"response"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&outer); err != nil {
		return AgentDiagnosis{}, err
	}
	var d AgentDiagnosis
	if err = json.Unmarshal([]byte(outer.Response), &d); err != nil {
		return AgentDiagnosis{}, err
	}
	d.Provider = "ollama"
	d.Model = p.Model
	d.IncidentID = c.Incident.ID
	d.ContextFingerprint = c.Fingerprint
	return d, nil
}
func agentPrompt(c ReliabilityAgentContext) string {
	return "Reliability diagnosis only. Select only provided runbook IDs. Never propose shell, kubectl, commands, approval, or execution. Do not claim causation beyond evidence. Return strict JSON with category, summary, evidence, confidence, candidateRunbooks, reasoningSummary. Context: " + mustAgentJSON(c)
}
func mustAgentJSON(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
