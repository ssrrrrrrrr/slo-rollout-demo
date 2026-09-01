package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReliabilityControllerSchedulesEveryServiceWithBoundedConcurrency(t *testing.T) {
	services := controllerTestServices(t, 3)
	controller := NewReliabilityController(services, &IncidentLifecycleService{}, true, time.Hour, 2)
	var calls, current, maximum int32
	var names sync.Map
	controller.reconcile = func(ctx context.Context, _ *http.Request, name string) (*ReliabilityIncident, error) {
		names.Store(name, true)
		atomic.AddInt32(&calls, 1)
		now := atomic.AddInt32(&current, 1)
		for {
			old := atomic.LoadInt32(&maximum)
			if now <= old || atomic.CompareAndSwapInt32(&maximum, old, now) {
				break
			}
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
		}
		atomic.AddInt32(&current, -1)
		if name == "service-2" {
			return nil, errors.New("provider unavailable")
		}
		return nil, nil
	}
	controller.ReconcileOnce(context.Background())
	status := controller.Status()
	if calls != 3 || maximum > 2 || status.ServicesEvaluated != 3 || status.ServicesSucceeded != 2 || status.ServicesFailed != 1 {
		t.Fatalf("unexpected bounded cycle: calls=%d max=%d status=%#v", calls, maximum, status)
	}
	for _, name := range []string{"demo-app", "service-2", "service-3"} {
		if _, ok := names.Load(name); !ok {
			t.Fatalf("service %s not reconciled", name)
		}
	}
}

func TestReliabilityControllerRunImmediatePeriodicAndStops(t *testing.T) {
	services := controllerTestServices(t, 1)
	controller := NewReliabilityController(services, &IncidentLifecycleService{}, true, 20*time.Millisecond, 1)
	var calls int32
	controller.reconcile = func(context.Context, *http.Request, string) (*ReliabilityIncident, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { controller.Run(ctx); close(done) }()
	deadline := time.After(time.Second)
	for atomic.LoadInt32(&calls) < 2 {
		select {
		case <-deadline:
			t.Fatal("controller did not reconcile immediately and periodically")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("controller did not stop after cancellation")
	}
	if controller.Status().Running {
		t.Fatal("controller still reports running")
	}
}

func TestControllerDurableIncidentIntegrationWithoutHTTP(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer func() { _ = h.repo.Close() }()
	h.service.readReconcile = false
	controller := NewReliabilityController(h.service.serviceService, h.lifecycle, true, time.Hour, 1)
	controller.ReconcileOnce(context.Background())
	active, _ := h.repo.FindActiveByService(context.Background(), "demo-app")
	if len(active) != 1 {
		t.Fatalf("controller did not create active incident: %#v", active)
	}
	first := active[0]
	controller.ReconcileOnce(context.Background())
	active, _ = h.repo.FindActiveByService(context.Background(), "demo-app")
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("controller duplicated active episode: %#v", active)
	}
	h.runtime.status = RuntimeStatusHealthy
	controller.ReconcileOnce(context.Background())
	resolved, _ := h.repo.Get(context.Background(), first.ID)
	if resolved.Status != IncidentStatusResolved {
		t.Fatalf("controller did not safely resolve: %#v", resolved)
	}
	_ = h.repo.Close()
	reopened, err := NewSQLiteIncidentRepository(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.repo = reopened
	h.lifecycle = NewIncidentLifecycleService(h.service, reopened)
	h.service.lifecycle = h.lifecycle
	h.runtime.status = RuntimeStatusUnhealthy
	restarted := NewReliabilityController(h.service.serviceService, h.lifecycle, true, time.Hour, 1)
	restarted.ReconcileOnce(context.Background())
	active, _ = reopened.FindActiveByService(context.Background(), "demo-app")
	if len(active) != 1 || active[0].ID == first.ID {
		t.Fatalf("restart recurrence did not create new episode: %#v", active)
	}
}

func TestControllerEnabledReadsRepositoryAndManualReconcile(t *testing.T) {
	h := newDurableIncidentHarness(t)
	defer h.repo.Close()
	ctx := context.Background()
	incident, err := h.lifecycle.ReconcileService(ctx, httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
	if err != nil || incident == nil {
		t.Fatal(err)
	}
	h.service.readReconcile = false
	h.runtime.status = RuntimeStatusHealthy
	controller := NewReliabilityController(h.service.serviceService, h.lifecycle, true, time.Hour, 1)
	api := &portalAPI{incidentSvc: h.service, controller: controller, cfg: Config{ReliabilityControllerEnabled: true, ReliabilityReconcileInterval: "1h", ReliabilityReconcileConcurrency: 1}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/incidents", api.handleIncidentList)
	mux.HandleFunc("/api/v1/services/", api.handleServiceDetail)
	mux.HandleFunc("/api/v1/controller/status", api.handleControllerStatus)
	response := callIncidentAPI(t, mux, "/api/v1/incidents", http.StatusOK)
	if response["count"] != float64(1) {
		t.Fatalf("controller-enabled GET forced reconcile instead of reading repository: %#v", response)
	}
	status := callIncidentAPI(t, mux, "/api/v1/controller/status", http.StatusOK)
	if status["enabled"] != true || status["concurrency"] != float64(1) {
		t.Fatalf("controller status: %#v", status)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/services/demo-app/reconcile", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("manual reconcile: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/services/missing/reconcile", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown manual reconcile: %d %s", recorder.Code, recorder.Body.String())
	}
}

func controllerTestServices(t *testing.T, count int) *ServiceService {
	t.Helper()
	repoDir := t.TempDir()
	dir := filepath.Join(repoDir, "configs", "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		name := "demo-app"
		if i > 0 {
			name = "service-" + string(rune('1'+i))
		}
		data := strings.ReplaceAll(testServiceConfig, "demo-app", name)
		if err := os.WriteFile(filepath.Join(dir, name+".service.yaml"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return NewServiceService(repoDir, nil)
}
