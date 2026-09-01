package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestLifecycleSerializesConcurrentSameServiceReconcile(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int32
	h.lifecycle.beforeReconcile = func(_ context.Context, _ string) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
		}
	}
	results := make(chan *ReliabilityIncident, 2)
	failures := make(chan error, 2)
	for range 2 {
		go func() {
			incident, err := h.lifecycle.ReconcileService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
			if err != nil {
				failures <- err
				return
			}
			results <- incident
		}()
	}
	<-entered
	close(release)
	first, second := <-results, <-results
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
	if first == nil || second == nil || first.ID != second.ID {
		t.Fatalf("same fingerprint must retain one episode: %#v %#v", first, second)
	}
	active, _ := h.repo.FindActiveByService(context.Background(), "demo-app")
	if len(active) != 1 {
		t.Fatalf("concurrent reconcile created duplicate episodes: %#v", active)
	}
	events, _ := h.repo.ListEvents(context.Background(), first.ID)
	if incidentEventCount(events, "INCIDENT_DETECTED") != 1 {
		t.Fatalf("duplicate incident timeline event: %#v", events)
	}
}

func TestLifecycleAllowsDifferentServiceReconcileConcurrency(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	payment := strings.ReplaceAll(testServiceConfig, "demo-app", "payment-service")
	if err := os.WriteFile(filepath.Join(h.service.serviceService.configDir, "payment.service.yaml"), []byte(payment), 0o600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan string, 2)
	permits := map[string]chan struct{}{
		"demo-app":        make(chan struct{}),
		"payment-service": make(chan struct{}),
	}
	h.lifecycle.beforeReconcile = func(_ context.Context, name string) {
		entered <- name
		<-permits[name]
	}
	type reconcileResult struct {
		name string
		err  error
	}
	done := make(chan reconcileResult, 2)
	for _, name := range []string{"demo-app", "payment-service"} {
		go func(name string) {
			_, err := h.lifecycle.ReconcileService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), name)
			done <- reconcileResult{name: name, err: err}
		}(name)
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-entered:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("different services were globally serialized")
		}
	}
	// Both transactions have entered their independent service gates. Let the
	// SQLite-backed test fixture persist them one at a time; SQLite's writer
	// serialization is unrelated to the lifecycle service-keyed gates.
	for _, name := range []string{"demo-app", "payment-service"} {
		close(permits[name])
		result := <-done
		if result.name != name || result.err != nil {
			t.Fatalf("reconcile result = %#v, want successful %s result", result, name)
		}
	}
}

func TestLifecycleReconcileCancellationWhileWaitingForServiceGate(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	h.lifecycle.beforeReconcile = func(_ context.Context, _ string) {
		select {
		case <-entered:
		default:
			close(entered)
			<-release
		}
	}
	first := make(chan error, 1)
	go func() {
		_, err := h.lifecycle.ReconcileService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
		first <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := h.lifecycle.ReconcileService(ctx, httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting reconcile did not honor context cancellation: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestControllerAndManualReconcileShareServiceGate(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls int32
	h.lifecycle.beforeReconcile = func(_ context.Context, _ string) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
		}
	}
	controller := NewReliabilityController(h.service.serviceService, h.lifecycle, true, time.Hour, 1)
	controllerDone := make(chan struct{})
	go func() { controller.ReconcileOnce(context.Background()); close(controllerDone) }()
	<-entered
	type manualResult struct {
		status int
	}
	manual := make(chan manualResult, 1)
	api := &portalAPI{incidentSvc: h.service}
	go func() {
		recorder := httptest.NewRecorder()
		api.handleManualServiceReconcile(recorder, httptest.NewRequest(http.MethodPost, "/", nil), "demo-app")
		manual <- manualResult{status: recorder.Code}
	}()
	close(release)
	<-controllerDone
	result := <-manual
	if result.status != http.StatusOK {
		t.Fatalf("manual reconcile status = %d, want %d", result.status, http.StatusOK)
	}
	active, _ := h.repo.FindActiveByService(context.Background(), "demo-app")
	if len(active) != 1 {
		t.Fatalf("controller/manual race created duplicate episode: %#v", active)
	}
	events, _ := h.repo.ListEvents(context.Background(), active[0].ID)
	if incidentEventCount(events, "INCIDENT_DETECTED") != 1 {
		t.Fatalf("controller/manual race duplicated incident timeline event: %#v", events)
	}
}

func incidentEventCount(events []IncidentTimelineEvent, wanted string) int {
	count := 0
	for _, event := range events {
		if event.Type == wanted {
			count++
		}
	}
	return count
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
