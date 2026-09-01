package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteIncidentRepositoryPersistsTimelineAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incidents.db")
	repo, err := NewSQLiteIncidentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	incident := ReliabilityIncident{ID: "INC-test", Fingerprint: "IFP-test", Service: "demo-app", Status: IncidentStatusActive, Severity: IncidentSeveritySEV3, Title: "Runtime unhealthy", PrimarySignal: IncidentSignal{Type: "RUNTIME_UNHEALTHY"}, FirstObservedAt: "2026-01-01T00:00:00Z", LastObservedAt: "2026-01-01T00:00:00Z", StartedAt: "2026-01-01T00:00:00Z", ObservedAt: "2026-01-01T00:00:00Z", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}
	if err := repo.Create(context.Background(), incident); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendEvent(context.Background(), incident.ID, IncidentTimelineEvent{Type: "FIRST", Message: "first", OccurredAt: "2026-01-01T00:00:02Z"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendEvent(context.Background(), incident.ID, IncidentTimelineEvent{Type: "SECOND", Message: "second", OccurredAt: "2026-01-01T00:00:03Z"}); err != nil {
		t.Fatal(err)
	}
	_ = repo.Close()
	reopened, err := NewSQLiteIncidentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Get(context.Background(), incident.ID)
	if err != nil || got.Fingerprint != incident.Fingerprint {
		t.Fatalf("reopen get: %#v %v", got, err)
	}
	events, err := reopened.ListEvents(context.Background(), incident.ID)
	if err != nil || len(events) != 2 || events[0].Type != "FIRST" {
		t.Fatalf("ordered events: %#v %v", events, err)
	}
}

func TestDurableIncidentLifecycleEpisodeAndRecovery(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer func() { _ = h.repo.Close() }()
	ctx, request := context.Background(), httptest.NewRequest(http.MethodGet, "/", nil)
	first, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || first == nil || first.Status != IncidentStatusActive {
		t.Fatalf("active incident: %#v %v", first, err)
	}
	again, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || again.ID != first.ID {
		t.Fatalf("identical reconcile must retain episode: %#v %v", again, err)
	}
	h.lifecycle.AgentAnalysisCompleted(ctx, first.ID, AgentDiagnosis{Category: AgentDiagnosisRuntimeFailure, Summary: "runtime failure", Confidence: .8, Provider: "deterministic"})
	h.lifecycle.RecoveryApproved(ctx, first.ID, RecoveryPlan{OperationID: "OP-1", Action: RunbookAction{Type: RecoveryRestartWorkload}, Target: RecoveryTarget{Namespace: "slo-rollout", Kind: "Rollout", Name: "demo-app"}})
	op := ControlledOperation{ID: "OP-1", Source: OperationSource{Type: "INCIDENT", ID: first.ID}, Action: OperationRestartWorkload, Target: OperationTarget{Service: "demo-app"}}
	h.lifecycle.OperationBlocked(ctx, op, "approval missing")
	blocked, _ := h.repo.Get(ctx, first.ID)
	if blocked.Status != IncidentStatusActive {
		t.Fatalf("blocked operation must retain ACTIVE: %#v", blocked)
	}
	h.lifecycle.OperationStarted(ctx, op)
	h.lifecycle.RecoveryVerification(ctx, first.ID, RecoveryVerification{Status: RecoveryVerificationRecovering, Reason: "workload restarting"})
	h.lifecycle.RecoveryVerification(ctx, first.ID, RecoveryVerification{Status: RecoveryVerificationRecovered, Reason: "healthy"})
	resolved, err := h.repo.Get(ctx, first.ID)
	if err != nil || resolved.Status != IncidentStatusResolved {
		t.Fatalf("resolved: %#v %v", resolved, err)
	}
	events, _ := h.repo.ListEvents(ctx, first.ID)
	if len(events) < 5 {
		t.Fatalf("expected durable lifecycle timeline, got %#v", events)
	}
	if !containsIncidentEvent(events, "AGENT_ANALYSIS_COMPLETED") || !containsIncidentEvent(events, "RECOVERY_APPROVED") {
		t.Fatalf("missing safe integration events: %#v", events)
	}
	_ = h.repo.Close()
	h.repo, err = NewSQLiteIncidentRepository(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.service.lifecycle = NewIncidentLifecycleService(h.service, h.repo)
	h.lifecycle = h.service.lifecycle
	h.runtime.status = RuntimeStatusUnhealthy
	next, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || next == nil || next.ID == first.ID {
		t.Fatalf("resolved fingerprint recurrence must create a new episode: %#v first=%s err=%v", next, first.ID, err)
	}
}

func TestLifecycleNeverResolvesOnUnknownProvider(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	ctx := context.Background()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	incident, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || incident == nil {
		t.Fatal(err)
	}
	h.runtime.status = RuntimeStatusUnknown
	h.slo.status = SLOStatusUnknown
	got, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || got == nil || got.ID != incident.ID {
		t.Fatalf("unknown providers must retain active incident: %#v %v", got, err)
	}
}

func TestLifecycleUpdatesSignalsAndSeverityWithoutDuplicatingEpisode(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	ctx, request := context.Background(), httptest.NewRequest(http.MethodGet, "/", nil)
	first, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || first == nil {
		t.Fatal(err)
	}
	h.slo.status = SLOStatusBreached
	updated, err := h.service.ActiveForService(ctx, request, "demo-app")
	if err != nil || updated == nil || updated.ID != first.ID || updated.Severity != IncidentSeveritySEV1 {
		t.Fatalf("signal update should retain episode: %#v %v", updated, err)
	}
	events, _ := h.repo.ListEvents(ctx, first.ID)
	count := len(events)
	if !containsIncidentEvent(events, "SIGNALS_CHANGED") || !containsIncidentEvent(events, "SEVERITY_CHANGED") {
		t.Fatalf("missing state-change events: %#v", events)
	}
	if _, err := h.service.ActiveForService(ctx, request, "demo-app"); err != nil {
		t.Fatal(err)
	}
	events, _ = h.repo.ListEvents(ctx, first.ID)
	if len(events) != count {
		t.Fatalf("identical reconcile spammed timeline: before=%d after=%d", count, len(events))
	}
}

func TestDurableIncidentAPIHistoryAndTimeline(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	incident, err := h.service.ActiveForService(context.Background(), request, "demo-app")
	if err != nil || incident == nil {
		t.Fatal(err)
	}
	api := &portalAPI{incidentSvc: h.service}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/incidents", api.handleIncidentList)
	mux.HandleFunc("/api/v1/incidents/", api.handleIncidentDetail)
	list := callIncidentAPI(t, mux, "/api/v1/incidents?state=ACTIVE", http.StatusOK)
	if list["count"] != float64(1) {
		t.Fatalf("active list compatibility: %#v", list)
	}
	timeline := callIncidentAPI(t, mux, "/api/v1/incidents/"+incident.ID+"/timeline", http.StatusOK)
	if timeline["schemaVersion"] != "incident.timeline/v1alpha1" || len(timeline["items"].([]interface{})) == 0 {
		t.Fatalf("timeline endpoint: %#v", timeline)
	}
	h.lifecycle.RecoveryVerification(context.Background(), incident.ID, RecoveryVerification{Status: RecoveryVerificationRecovered, Reason: "healthy"})
	h.runtime.status = RuntimeStatusHealthy
	history := callIncidentAPI(t, mux, "/api/v1/incidents?includeResolved=true", http.StatusOK)
	if history["count"] != float64(1) {
		t.Fatalf("resolved history query: %#v", history)
	}
}

func containsIncidentEvent(events []IncidentTimelineEvent, wanted string) bool {
	for _, event := range events {
		if event.Type == wanted {
			return true
		}
	}
	return false
}

type durableIncidentHarness struct {
	service   *IncidentService
	lifecycle *IncidentLifecycleService
	repo      *SQLiteIncidentRepository
	path      string
	runtime   *mutableRuntimeProvider
	slo       *mutableSLOProvider
}

func newDurableIncidentHarness(t *testing.T) *durableIncidentHarness {
	t.Helper()
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	services := NewServiceService(repoDir, nil)
	runtime := &mutableRuntimeProvider{status: RuntimeStatusUnhealthy}
	slo := &mutableSLOProvider{status: SLOStatusHealthy}
	service := NewIncidentService(services, NewSLOService(repoDir, services, slo), NewRuntimeService(services, runtime), NewReliabilityIncidentDetector())
	path := filepath.Join(t.TempDir(), "incidents.db")
	repo, err := NewSQLiteIncidentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewIncidentLifecycleService(service, repo)
	lifecycle.now = func() time.Time { return time.Now().UTC() }
	service.lifecycle = lifecycle
	return &durableIncidentHarness{service, lifecycle, repo, path, runtime, slo}
}

type mutableRuntimeProvider struct{ status RuntimeStatus }

func (p *mutableRuntimeProvider) Snapshot(_ context.Context, service Service) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{Service: service.Metadata.Name, Status: p.status, ObservedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}

type mutableSLOProvider struct{ status SLOStatus }

func (p *mutableSLOProvider) Evaluate(_ context.Context, service Service, _ ServiceSLOConfig) (ServiceSLOStatus, error) {
	return ServiceSLOStatus{Service: service.Metadata.Name, Status: p.status, EvaluatedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
