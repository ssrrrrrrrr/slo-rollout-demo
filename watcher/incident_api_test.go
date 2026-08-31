package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReliabilityIncidentDetectionRules(t *testing.T) {
	tests := []struct {
		name            string
		slo             SLOStatus
		runtime         RuntimeStatus
		release         string
		wantIncident    bool
		wantSeverity    IncidentSeverity
		wantPrimaryType string
	}{
		{"SLO breached creates incident", SLOStatusBreached, RuntimeStatusHealthy, "PASS", true, IncidentSeveritySEV2, "SLO_BREACH"},
		{"runtime unhealthy creates incident", SLOStatusHealthy, RuntimeStatusUnhealthy, "PASS", true, IncidentSeveritySEV3, "RUNTIME_UNHEALTHY"},
		{"SLO at risk alone does not create incident", SLOStatusAtRisk, RuntimeStatusHealthy, "PASS", false, "", ""},
		{"runtime degraded alone does not create incident", SLOStatusHealthy, RuntimeStatusDegraded, "PASS", false, "", ""},
		{"SLO breach and runtime unhealthy is SEV1", SLOStatusBreached, RuntimeStatusUnhealthy, "PASS", true, IncidentSeveritySEV1, "SLO_BREACH"},
		{"release failure creates SEV3 incident", SLOStatusHealthy, RuntimeStatusHealthy, "FAILED", true, IncidentSeveritySEV3, "RELEASE_FAILED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			incident := NewReliabilityIncidentDetector().Detect(testIncidentInput(test.slo, test.runtime, test.release))
			if (incident != nil) != test.wantIncident {
				t.Fatalf("incident presence: got %#v, want %v", incident, test.wantIncident)
			}
			if incident == nil {
				return
			}
			if incident.Severity != test.wantSeverity || incident.PrimarySignal.Type != test.wantPrimaryType {
				t.Fatalf("unexpected incident: %#v", incident)
			}
		})
	}
}

func TestIncidentCorrelationIDAndTimeline(t *testing.T) {
	input := testIncidentInput(SLOStatusBreached, RuntimeStatusDegraded, "PAUSED")
	first := NewReliabilityIncidentDetector().Detect(input)
	second := NewReliabilityIncidentDetector().Detect(input)
	if first == nil || second == nil || first.ID != second.ID {
		t.Fatalf("incident ID must be deterministic: first=%#v second=%#v", first, second)
	}
	if first.RelatedRelease == nil || first.RelatedRelease.ID != "release-123" || first.RelatedRelease.Correlation != "TEMPORAL" {
		t.Fatalf("expected temporal release correlation, got %#v", first.RelatedRelease)
	}
	if len(first.Timeline) < 3 || first.Timeline[0].Type != "SLO_BREACH" {
		t.Fatalf("expected synthesized SLO, Runtime, and Release timeline, got %#v", first.Timeline)
	}
}

func TestIncidentReleaseFreshness(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	detector := NewReliabilityIncidentDetectorWithFreshnessWindow(time.Hour)
	detector.now = func() time.Time { return now }

	t.Run("recent failed release creates incident", func(t *testing.T) {
		input := testIncidentInput(SLOStatusHealthy, RuntimeStatusHealthy, "FAILED")
		input.LatestRelease.Timestamp = now.Add(-30 * time.Minute).Format(time.RFC3339)
		incident := detector.Detect(input)
		if incident == nil || incident.PrimarySignal.Type != "RELEASE_FAILED" {
			t.Fatalf("expected recent failed release incident, got %#v", incident)
		}
	})

	t.Run("stale failed release alone creates no incident", func(t *testing.T) {
		input := testIncidentInput(SLOStatusHealthy, RuntimeStatusHealthy, "FAILED")
		input.LatestRelease.Timestamp = now.Add(-2 * time.Hour).Format(time.RFC3339)
		if incident := detector.Detect(input); incident != nil {
			t.Fatalf("expected no incident for stale release, got %#v", incident)
		}
	})

	t.Run("stale failed release with SLO breach stays SLO primary", func(t *testing.T) {
		input := testIncidentInput(SLOStatusBreached, RuntimeStatusHealthy, "FAILED")
		input.LatestRelease.Timestamp = now.Add(-2 * time.Hour).Format(time.RFC3339)
		incident := detector.Detect(input)
		if incident == nil || incident.PrimarySignal.Type != "SLO_BREACH" || incident.Severity != IncidentSeveritySEV2 {
			t.Fatalf("expected SLO-driven SEV2 incident, got %#v", incident)
		}
	})

	t.Run("stale failed release with runtime unhealthy remains SEV3", func(t *testing.T) {
		input := testIncidentInput(SLOStatusHealthy, RuntimeStatusUnhealthy, "FAILED")
		input.LatestRelease.Timestamp = now.Add(-2 * time.Hour).Format(time.RFC3339)
		incident := detector.Detect(input)
		if incident == nil || incident.PrimarySignal.Type != "RUNTIME_UNHEALTHY" || incident.Severity != IncidentSeveritySEV3 {
			t.Fatalf("expected Runtime-driven SEV3 incident, got %#v", incident)
		}
	})

	t.Run("failed release without timestamp creates no incident", func(t *testing.T) {
		input := testIncidentInput(SLOStatusHealthy, RuntimeStatusHealthy, "FAILED")
		input.LatestRelease.Timestamp = ""
		if incident := detector.Detect(input); incident != nil {
			t.Fatalf("expected no incident for release without timestamp, got %#v", incident)
		}
	})
}

func TestIncidentServiceAndAPI(t *testing.T) {
	service := newIncidentTestService(t, SLOStatusBreached, RuntimeStatusDegraded, "PAUSED")
	api := &portalAPI{incidentSvc: service}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/incidents", api.handleIncidentList)
	mux.HandleFunc("/api/v1/incidents/", api.handleIncidentDetail)
	mux.HandleFunc("/api/v1/services/", api.handleServiceDetail)

	list := callIncidentAPI(t, mux, "/api/v1/incidents", http.StatusOK)
	items := list["items"].([]interface{})
	if list["schemaVersion"] != "incident.list/v1alpha1" || len(items) != 1 {
		t.Fatalf("unexpected incident list: %#v", list)
	}
	incident := items[0].(map[string]interface{})
	id := incident["id"].(string)

	detail := callIncidentAPI(t, mux, "/api/v1/incidents/"+id, http.StatusOK)
	if detail["schemaVersion"] != "incident/v1alpha1" || detail["incident"].(map[string]interface{})["id"] != id {
		t.Fatalf("unexpected incident detail: %#v", detail)
	}

	serviceItems := callIncidentAPI(t, mux, "/api/v1/services/demo-app/incidents", http.StatusOK)
	if serviceItems["count"] != float64(1) {
		t.Fatalf("unexpected service incident list: %#v", serviceItems)
	}
	active := callIncidentAPI(t, mux, "/api/v1/services/demo-app/incidents/active", http.StatusOK)
	if active["incident"].(map[string]interface{})["id"] != id {
		t.Fatalf("unexpected active incident: %#v", active)
	}

	unknown := callIncidentAPI(t, mux, "/api/v1/services/missing/incidents/active", http.StatusNotFound)
	if unknown["error"] != "service not found" {
		t.Fatalf("unexpected unknown-service response: %#v", unknown)
	}
}

func TestIncidentServiceNoIncidentAndUnavailableProviders(t *testing.T) {
	t.Run("no incident", func(t *testing.T) {
		service := newIncidentTestService(t, SLOStatusAtRisk, RuntimeStatusDegraded, "PASS")
		incident, err := service.ActiveForService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
		if err != nil || incident != nil {
			t.Fatalf("expected no incident, got %#v err=%v", incident, err)
		}

		api := &portalAPI{incidentSvc: service}
		body := callIncidentAPI(t, http.HandlerFunc(api.handleServiceDetail), "/api/v1/services/demo-app/incidents/active", http.StatusOK)
		if body["incident"] != nil {
			t.Fatalf("expected active endpoint to return a null incident, got %#v", body)
		}
	})

	t.Run("unavailable SLO and Runtime providers do not panic", func(t *testing.T) {
		repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
		repository := &incidentTestRepository{releaseStatus: "PASS"}
		serviceService := NewServiceService(repoDir, repository)
		service := NewIncidentService(
			serviceService,
			NewSLOService(repoDir, serviceService, &unavailableIncidentSLOProvider{}),
			NewRuntimeService(serviceService, &KubernetesRuntimeProvider{initErr: errors.New("cluster unavailable")}),
			NewReliabilityIncidentDetector(),
		)
		incident, err := service.ActiveForService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
		if err != nil || incident != nil {
			t.Fatalf("unavailable providers must return no fabricated incident, got %#v err=%v", incident, err)
		}
	})
}

func testIncidentInput(sloStatus SLOStatus, runtimeStatus RuntimeStatus, releaseStatus string) IncidentDetectionInput {
	now := time.Now().UTC()
	return IncidentDetectionInput{
		Service:       Service{Metadata: ServiceMetadata{Name: "demo-app"}},
		SLO:           ServiceSLOStatus{Service: "demo-app", Status: sloStatus, EvaluatedAt: now.Add(-2 * time.Minute).Format(time.RFC3339)},
		Runtime:       RuntimeSnapshot{Service: "demo-app", Status: runtimeStatus, ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), Pods: []RuntimePodStatus{}},
		LatestRelease: &ServiceReleaseSummary{ID: "release-123", Status: releaseStatus, Timestamp: now.Format(time.RFC3339)},
	}
}

func newIncidentTestService(t *testing.T, sloStatus SLOStatus, runtimeStatus RuntimeStatus, releaseStatus string) *IncidentService {
	t.Helper()
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	serviceService := NewServiceService(repoDir, &incidentTestRepository{releaseStatus: releaseStatus})
	return NewIncidentService(
		serviceService,
		NewSLOService(repoDir, serviceService, &staticSLOProvider{status: ServiceSLOStatus{Service: "demo-app", Status: sloStatus, EvaluatedAt: "2026-08-31T00:00:00Z"}}),
		NewRuntimeService(serviceService, &staticRuntimeProvider{snapshot: RuntimeSnapshot{Service: "demo-app", Status: runtimeStatus, ObservedAt: "2026-08-31T00:01:00Z", Pods: []RuntimePodStatus{}}}),
		NewReliabilityIncidentDetector(),
	)
}

func callIncidentAPI(t *testing.T, handler http.Handler, target string, expectedStatus int) map[string]interface{} {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != expectedStatus {
		t.Fatalf("%s: expected HTTP %d, got %d: %s", target, expectedStatus, recorder.Code, recorder.Body.String())
	}
	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode incident response: %v", err)
	}
	return body
}

type incidentTestRepository struct {
	releaseStatus string
}

func (repo *incidentTestRepository) Descriptor() EvidenceRepositoryDescriptor {
	return EvidenceRepositoryDescriptor{}
}
func (repo *incidentTestRepository) ListReleases(_ *http.Request, _ EvidenceReleaseListQuery) (*EvidenceRepositoryResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{"items": []map[string]interface{}{{"release_id": "release-123", "release_result": repo.releaseStatus, "generated_at": "2026-08-31T00:02:00Z"}}})
	return &EvidenceRepositoryResponse{Body: body}, nil
}
func (repo *incidentTestRepository) GetRelease(*http.Request, EvidenceReleaseQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *incidentTestRepository) GetObject(*http.Request, EvidenceObjectQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *incidentTestRepository) ListArtifacts(*http.Request, EvidenceArtifactListQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *incidentTestRepository) SearchObjects(*http.Request, EvidenceSearchQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *incidentTestRepository) GetVerificationSummary(*http.Request, EvidenceVerificationSummaryQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *incidentTestRepository) GetGraph(*http.Request, EvidenceGraphQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}

type unavailableIncidentSLOProvider struct{}

func (*unavailableIncidentSLOProvider) Evaluate(context.Context, Service, ServiceSLOConfig) (ServiceSLOStatus, error) {
	return ServiceSLOStatus{}, errors.New("Prometheus unavailable")
}
