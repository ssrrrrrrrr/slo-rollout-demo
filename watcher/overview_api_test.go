package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReliabilityOverviewFleetSummary(t *testing.T) {
	svc := newOverviewTestService(t, []string{"healthy", "at-risk", "unhealthy"}, map[string]SLOStatus{
		"healthy": SLOStatusHealthy, "at-risk": SLOStatusAtRisk, "unhealthy": SLOStatusBreached,
	}, map[string]RuntimeStatus{
		"healthy": RuntimeStatusHealthy, "at-risk": RuntimeStatusHealthy, "unhealthy": RuntimeStatusDegraded,
	})
	overview, err := svc.Get(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("get mixed overview: %v", err)
	}
	if overview.Summary.TotalServices != 3 || overview.Summary.HealthyServices != 1 || overview.Summary.AtRiskServices != 1 || overview.Summary.UnhealthyServices != 1 {
		t.Fatalf("unexpected fleet status counts: %#v", overview.Summary)
	}
	if overview.Summary.ActiveIncidents != 1 || overview.Summary.SEV2Incidents != 1 || overview.Summary.SLOBreaches != 1 || overview.Summary.RuntimeDegraded != 1 {
		t.Fatalf("unexpected fleet issue counts: %#v", overview.Summary)
	}
}

func TestReliabilityOverviewAllHealthyAndMultipleServices(t *testing.T) {
	svc := newOverviewTestService(t, []string{"api", "worker"}, map[string]SLOStatus{"api": SLOStatusHealthy, "worker": SLOStatusHealthy}, map[string]RuntimeStatus{"api": RuntimeStatusHealthy, "worker": RuntimeStatusHealthy})
	overview, err := svc.Get(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("get healthy overview: %v", err)
	}
	if overview.Summary.TotalServices != 2 || overview.Summary.HealthyServices != 2 || overview.Summary.ActiveIncidents != 0 || len(overview.Attention) != 0 {
		t.Fatalf("unexpected healthy overview: %#v attention=%#v", overview.Summary, overview.Attention)
	}
}

func TestReliabilityOverviewReleaseRiskFreshness(t *testing.T) {
	tests := []struct {
		name          string
		timestamp     string
		wantRisk      int
		wantStatus    ReliabilityOverallStatus
		wantAttention bool
	}{
		{"recent failed release is current risk", time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339), 1, ReliabilityOverallAtRisk, true},
		{"stale failed release is not current risk", time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339), 0, ReliabilityOverallHealthy, false},
		{"failed release without timestamp is not current risk", "", 0, ReliabilityOverallHealthy, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := newOverviewTestService(t, []string{"demo-app"}, map[string]SLOStatus{"demo-app": SLOStatusHealthy}, map[string]RuntimeStatus{"demo-app": RuntimeStatusHealthy})
			repository := svc.serviceService.repository.(*overviewTestRepository)
			repository.releaseStatus = "FAILED"
			repository.releaseTimestamp = test.timestamp

			overview, err := svc.Get(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatalf("get overview: %v", err)
			}
			service := overview.Services[0]
			if overview.Summary.ReleaseRisks != test.wantRisk || service.OverallStatus != test.wantStatus || (len(overview.Attention) > 0) != test.wantAttention {
				t.Fatalf("unexpected freshness overview: summary=%#v service=%#v attention=%#v", overview.Summary, service, overview.Attention)
			}
			if service.LatestRelease == nil || service.LatestRelease.Status != "FAILED" || service.LatestRelease.Timestamp != test.timestamp {
				t.Fatalf("failed release must remain visible as latest summary: %#v", service.LatestRelease)
			}
		})
	}

	t.Run("stale failed release does not downgrade unknown service", func(t *testing.T) {
		svc := newOverviewTestService(t, []string{"demo-app"}, map[string]SLOStatus{"demo-app": SLOStatusUnknown}, map[string]RuntimeStatus{"demo-app": RuntimeStatusHealthy})
		repository := svc.serviceService.repository.(*overviewTestRepository)
		repository.releaseStatus = "FAILED"
		repository.releaseTimestamp = time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
		overview, err := svc.Get(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
		if err != nil || overview.Services[0].OverallStatus != ReliabilityOverallUnknown || overview.Summary.ReleaseRisks != 0 || len(overview.Attention) != 0 {
			t.Fatalf("stale release must not alter UNKNOWN service state: overview=%#v err=%v", overview, err)
		}
	})
}

func TestReliabilityOverviewStatusPrecedence(t *testing.T) {
	budget := 10.0
	tests := []struct {
		name    string
		summary ServiceReliabilitySummary
		want    ReliabilityOverallStatus
	}{
		{"SEV2 incident beats healthy states", ServiceReliabilitySummary{SLOStatus: SLOStatusHealthy, RuntimeStatus: RuntimeStatusHealthy, IncidentStatus: IncidentStatusActive, IncidentSeverity: IncidentSeveritySEV2}, ReliabilityOverallUnhealthy},
		{"SLO breach beats degraded runtime", ServiceReliabilitySummary{SLOStatus: SLOStatusBreached, RuntimeStatus: RuntimeStatusDegraded}, ReliabilityOverallUnhealthy},
		{"SEV3 incident is at risk", ServiceReliabilitySummary{SLOStatus: SLOStatusHealthy, RuntimeStatus: RuntimeStatusHealthy, IncidentStatus: IncidentStatusActive, IncidentSeverity: IncidentSeveritySEV3}, ReliabilityOverallAtRisk},
		{"low error budget is at risk", ServiceReliabilitySummary{SLOStatus: SLOStatusHealthy, RuntimeStatus: RuntimeStatusHealthy, ErrorBudgetRemaining: &budget}, ReliabilityOverallAtRisk},
		{"healthy requires SLO and runtime", ServiceReliabilitySummary{SLOStatus: SLOStatusHealthy, RuntimeStatus: RuntimeStatusHealthy}, ReliabilityOverallHealthy},
		{"unknown data remains unknown", ServiceReliabilitySummary{SLOStatus: SLOStatusUnknown, RuntimeStatus: RuntimeStatusHealthy}, ReliabilityOverallUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := overallServiceStatus(test.summary); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}

func TestReliabilityOverviewAttentionOrderingAndProviderFailure(t *testing.T) {
	svc := newOverviewTestService(t, []string{"critical", "at-risk", "unknown"}, map[string]SLOStatus{
		"critical": SLOStatusBreached, "at-risk": SLOStatusAtRisk, "unknown": SLOStatusHealthy,
	}, map[string]RuntimeStatus{
		"critical": RuntimeStatusUnhealthy, "at-risk": RuntimeStatusHealthy, "unknown": RuntimeStatusHealthy,
	})
	svc.sloService.provider = &overviewSLOProvider{statuses: map[string]SLOStatus{"critical": SLOStatusBreached, "at-risk": SLOStatusAtRisk}, errors: map[string]error{"unknown": errors.New("Prometheus unavailable")}}
	overview, err := svc.Get(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("single-service provider failure must not fail overview: %v", err)
	}
	if overview.Services[2].OverallStatus != ReliabilityOverallUnknown || overview.Summary.UnknownServices != 1 {
		t.Fatalf("expected unavailable service to be UNKNOWN, got %#v", overview)
	}
	if len(overview.Attention) != 2 || overview.Attention[0].Service != "critical" || overview.Attention[0].Priority != "CRITICAL" {
		t.Fatalf("unexpected attention order: %#v", overview.Attention)
	}
}

func TestReliabilityOverviewAPI(t *testing.T) {
	svc := newOverviewTestService(t, []string{"demo-app"}, map[string]SLOStatus{"demo-app": SLOStatusHealthy}, map[string]RuntimeStatus{"demo-app": RuntimeStatusHealthy})
	api := &portalAPI{overviewSvc: svc}
	body := callOverviewAPI(t, http.HandlerFunc(api.handleReliabilityOverview), "/api/v1/overview", http.StatusOK)
	if body["schemaVersion"] != "reliability.overview/v1alpha1" || body["summary"].(map[string]interface{})["totalServices"] != float64(1) {
		t.Fatalf("unexpected overview API response: %#v", body)
	}
}

func newOverviewTestService(t *testing.T, names []string, sloStatuses map[string]SLOStatus, runtimeStatuses map[string]RuntimeStatus) *OverviewService {
	t.Helper()
	repoDir := writeOverviewTestRepo(t, names)
	serviceSvc := NewServiceService(repoDir, &overviewTestRepository{})
	sloSvc := NewSLOService(repoDir, serviceSvc, &overviewSLOProvider{statuses: sloStatuses})
	runtimeSvc := NewRuntimeService(serviceSvc, &overviewRuntimeProvider{statuses: runtimeStatuses})
	incidentSvc := NewIncidentService(serviceSvc, sloSvc, runtimeSvc, NewReliabilityIncidentDetector())
	return NewOverviewService(serviceSvc, sloSvc, runtimeSvc, incidentSvc)
}

func writeOverviewTestRepo(t *testing.T, names []string) string {
	t.Helper()
	repoDir := t.TempDir()
	configDir := filepath.Join(repoDir, "configs", "services")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	for _, name := range names {
		service := fmt.Sprintf("apiVersion: sentinel.io/v1alpha1\nkind: Service\nmetadata:\n  name: %s\nspec:\n  displayName: %s\n  owner: platform-team\nenvironments: [dev]\nruntime:\n  namespace: slo-rollout\n  workload:\n    kind: Rollout\n    name: %s\nreliability:\n  sloRef: %s-slo\ndelivery:\n  strategyRef: standard\n", name, name, name, name)
		slo := fmt.Sprintf("apiVersion: slo.ssentinel.io/v1alpha1\nkind: SLOConfig\nmetadata:\n  name: %s-slo\nspec:\n  serviceLevel:\n    window: 30d\n    availabilityTarget: 99.9\n", name)
		if err := os.WriteFile(filepath.Join(configDir, name+".service.yaml"), []byte(service), 0644); err != nil {
			t.Fatalf("write service: %v", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, name+".slo.yaml"), []byte(slo), 0644); err != nil {
			t.Fatalf("write SLO: %v", err)
		}
	}
	return repoDir
}

func callOverviewAPI(t *testing.T, handler http.Handler, target string, expectedStatus int) map[string]interface{} {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != expectedStatus {
		t.Fatalf("expected HTTP %d, got %d: %s", expectedStatus, recorder.Code, recorder.Body.String())
	}
	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode overview response: %v", err)
	}
	return body
}

type overviewSLOProvider struct {
	statuses map[string]SLOStatus
	errors   map[string]error
}

func (provider *overviewSLOProvider) Evaluate(_ context.Context, service Service, _ ServiceSLOConfig) (ServiceSLOStatus, error) {
	if err := provider.errors[service.Metadata.Name]; err != nil {
		return ServiceSLOStatus{}, err
	}
	status := provider.statuses[service.Metadata.Name]
	if status == "" {
		status = SLOStatusHealthy
	}
	return ServiceSLOStatus{Service: service.Metadata.Name, Status: status, EvaluatedAt: time.Now().Format(time.RFC3339), ErrorBudget: ErrorBudgetStatus{Status: status}, BurnRate: BurnRateStatus{Status: status}}, nil
}

type overviewRuntimeProvider struct{ statuses map[string]RuntimeStatus }

func (provider *overviewRuntimeProvider) Snapshot(_ context.Context, service Service) (RuntimeSnapshot, error) {
	status := provider.statuses[service.Metadata.Name]
	if status == "" {
		status = RuntimeStatusHealthy
	}
	return RuntimeSnapshot{Service: service.Metadata.Name, Status: status, ObservedAt: time.Now().Format(time.RFC3339), Pods: []RuntimePodStatus{}}, nil
}

type overviewTestRepository struct {
	releaseStatus    string
	releaseTimestamp string
}

func (*overviewTestRepository) Descriptor() EvidenceRepositoryDescriptor {
	return EvidenceRepositoryDescriptor{}
}
func (repo *overviewTestRepository) ListReleases(_ *http.Request, _ EvidenceReleaseListQuery) (*EvidenceRepositoryResponse, error) {
	status := repo.releaseStatus
	if status == "" {
		status = "PASS"
	}
	timestamp := repo.releaseTimestamp
	if timestamp == "" && status == "PASS" {
		timestamp = time.Now().Format(time.RFC3339)
	}
	body, _ := json.Marshal(map[string]interface{}{"items": []map[string]interface{}{{"release_id": "release-1", "release_result": status, "generated_at": timestamp}}})
	return &EvidenceRepositoryResponse{Body: body}, nil
}
func (*overviewTestRepository) GetRelease(*http.Request, EvidenceReleaseQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*overviewTestRepository) GetObject(*http.Request, EvidenceObjectQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*overviewTestRepository) ListArtifacts(*http.Request, EvidenceArtifactListQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*overviewTestRepository) SearchObjects(*http.Request, EvidenceSearchQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*overviewTestRepository) GetVerificationSummary(*http.Request, EvidenceVerificationSummaryQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*overviewTestRepository) GetGraph(*http.Request, EvidenceGraphQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
