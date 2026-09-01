package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type ReliabilityAgentService struct {
	incidents *IncidentService
	services  *ServiceService
	recovery  *RecoveryService
	provider  ReliabilityAgentProvider
	fallback  ReliabilityAgentProvider
	mu        sync.Mutex
	cache     map[string]agentAnalysisCacheEntry
}

func NewReliabilityAgentService(i *IncidentService, s *ServiceService, r *RecoveryService, p ReliabilityAgentProvider) *ReliabilityAgentService {
	return &ReliabilityAgentService{incidents: i, services: s, recovery: r, provider: p, fallback: DeterministicAgentProvider{}, cache: map[string]agentAnalysisCacheEntry{}}
}
func (api *portalAPI) agentService() *ReliabilityAgentService {
	if api.agentSvc != nil {
		return api.agentSvc
	}
	r := api.recoveryService()
	p := ReliabilityAgentProvider(OllamaReliabilityAgentProvider{URL: api.cfg.OllamaURL, Model: api.cfg.Model})
	a := NewReliabilityAgentService(api.incidentService(), api.serviceService(), r, p)
	api.agentSvc = a
	r.agent = a
	return a
}
func (s *ReliabilityAgentService) Context(ctx context.Context, r *http.Request, id string) (ReliabilityAgentContext, error) {
	i, e := s.incidents.Get(ctx, r, id)
	if e != nil {
		return ReliabilityAgentContext{}, e
	}
	service, e := s.services.find(i.Service)
	if e != nil {
		return ReliabilityAgentContext{}, e
	}
	books, e := s.recovery.Load()
	if e != nil {
		return ReliabilityAgentContext{}, e
	}
	var latest *ServiceReleaseSummary
	if i.RelatedRelease != nil {
		latest = &ServiceReleaseSummary{ID: i.RelatedRelease.ID, Status: i.RelatedRelease.Status, Timestamp: i.RelatedRelease.Timestamp}
	}
	c := ReliabilityAgentContext{Service: service, SLO: i.SLO, Runtime: i.Runtime, Incident: *i, LatestRelease: latest, ReleaseEvidence: i.ReleaseEvidence, Runbooks: books}
	c.Fingerprint = agentFingerprint(c)
	return c, nil
}
func agentFingerprint(c ReliabilityAgentContext) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(c.Incident.ID + "|" + string(c.SLO.Status) + "|" + string(c.Runtime.Status) + fmt.Sprintf("|%d|%d|", c.Runtime.Workload.ReadyReplicas, c.Runtime.Workload.DesiredReplicas)))
	for _, x := range c.Incident.Signals {
		_, _ = h.Write([]byte(x.Type + ":" + x.Status + "|"))
	}
	if c.LatestRelease != nil {
		_, _ = h.Write([]byte(c.LatestRelease.ID + ":" + c.LatestRelease.Status))
	}
	for _, b := range c.Runbooks {
		_, _ = h.Write([]byte("|" + b.Metadata.Name))
	}
	return fmt.Sprintf("af-%x", h.Sum64())
}
func (s *ReliabilityAgentService) Analyze(ctx context.Context, r *http.Request, id string) (AgentDiagnosis, error) {
	c, e := s.Context(ctx, r, id)
	if e != nil {
		return AgentDiagnosis{}, e
	}
	s.mu.Lock()
	if x, ok := s.cache[id]; ok && x.fingerprint == c.Fingerprint {
		s.mu.Unlock()
		return x.diagnosis, nil
	}
	s.mu.Unlock()
	started := time.Now().UTC()
	d, e := s.provider.Analyze(ctx, c)
	fallback := e != nil || !validateAgentDiagnosis(&d, c)
	if fallback {
		d, _ = s.fallback.Analyze(ctx, c)
		d.FallbackUsed = true
	}
	d.AnalysisID = "AA-" + c.Fingerprint
	d.StartedAt = started.Format(time.RFC3339)
	d.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	d.ContextFingerprint = c.Fingerprint
	s.mu.Lock()
	s.cache[id] = agentAnalysisCacheEntry{c.Fingerprint, d, time.Now()}
	s.mu.Unlock()
	return d, nil
}
func (s *ReliabilityAgentService) Cached(ctx context.Context, r *http.Request, id string) *AgentDiagnosis {
	c, e := s.Context(ctx, r, id)
	if e != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.cache[id]
	if !ok || x.fingerprint != c.Fingerprint {
		return nil
	}
	d := x.diagnosis
	return &d
}
func validateAgentDiagnosis(d *AgentDiagnosis, c ReliabilityAgentContext) bool {
	switch d.Category {
	case AgentDiagnosisReleaseFailure, AgentDiagnosisRuntimeFailure, AgentDiagnosisSLODegradation, AgentDiagnosisCapacityRisk, AgentDiagnosisMultiSignal, AgentDiagnosisUnknown:
	default:
		return false
	}
	if d.Confidence < 0 || d.Confidence > 1 {
		return false
	}
	allowed := map[string]bool{}
	for _, b := range c.Runbooks {
		allowed[b.Metadata.Name] = true
	}
	filtered := make([]AgentRunbookCandidate, 0, len(d.CandidateRunbooks))
	for _, x := range d.CandidateRunbooks {
		if allowed[x.ID] && x.Confidence >= 0 && x.Confidence <= 1 {
			filtered = append(filtered, x)
		}
	}
	d.CandidateRunbooks = filtered
	return strings.TrimSpace(d.Summary) != ""
}

var _ = os.Getenv
