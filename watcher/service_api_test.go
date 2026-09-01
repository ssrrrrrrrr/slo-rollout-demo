package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testServiceConfig = `apiVersion: sentinel.io/v1alpha1
kind: Service
metadata:
  name: demo-app
spec:
  displayName: Demo Application
  owner: platform-team
environments: [dev, staging, prod]
runtime:
  namespace: slo-rollout
  workload:
    kind: Rollout
    name: demo-app
reliability:
  sloRef: demo-app-canary-slo
delivery:
  strategyRef: demo-app-canary-strategy
`

func TestServiceConfigLoading(t *testing.T) {
	repoDir := writeTestServiceConfig(t)

	services, err := NewServiceService(repoDir, nil).Load()
	if err != nil {
		t.Fatalf("load services: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}

	service := services[0]
	if service.Metadata.Name != "demo-app" || service.Runtime.Workload.Name != "demo-app" || service.Reliability.SLORef != "demo-app-canary-slo" || service.Delivery.StrategyRef != "demo-app-canary-strategy" {
		t.Fatalf("unexpected loaded service: %#v", service)
	}
}

func TestServiceAPI(t *testing.T) {
	repo := &testServiceEvidenceRepository{}
	api := &portalAPI{
		serviceSvc: NewServiceService(writeTestServiceConfig(t), repo),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/services", api.handleServiceList)
	mux.HandleFunc("/api/v1/services/", api.handleServiceDetail)

	t.Run("list services", func(t *testing.T) {
		body := callServiceAPI(t, mux, "/api/v1/services", http.StatusOK)
		if body["count"] != float64(1) {
			t.Fatalf("expected one service, got %#v", body["count"])
		}
		items := body["items"].([]interface{})
		service := items[0].(map[string]interface{})
		if service["name"] != "demo-app" || service["displayName"] != "Demo Application" || service["owner"] != "platform-team" {
			t.Fatalf("unexpected service summary: %#v", service)
		}
		latestRelease := service["latestRelease"].(map[string]interface{})
		if latestRelease["id"] != "20260831-000001" || latestRelease["status"] != "PASS" || latestRelease["timestamp"] != "2026-08-31T00:00:01Z" {
			t.Fatalf("unexpected latest release: %#v", latestRelease)
		}
	})

	t.Run("get existing service", func(t *testing.T) {
		body := callServiceAPI(t, mux, "/api/v1/services/demo-app", http.StatusOK)
		service := body["service"].(map[string]interface{})
		if service["name"] != "demo-app" || service["sloRef"] != "demo-app-canary-slo" || service["strategyRef"] != "demo-app-canary-strategy" {
			t.Fatalf("unexpected service detail: %#v", service)
		}
	})

	t.Run("unknown service returns 404", func(t *testing.T) {
		body := callServiceAPI(t, mux, "/api/v1/services/missing", http.StatusNotFound)
		if body["error"] != "service not found" {
			t.Fatalf("unexpected error response: %#v", body)
		}
	})

	t.Run("service release association", func(t *testing.T) {
		body := callServiceAPI(t, mux, "/api/v1/services/demo-app/releases", http.StatusOK)
		items := body["items"].([]interface{})
		if len(items) != 2 {
			t.Fatalf("expected two associated releases, got %#v", items)
		}
		if repo.lastQuery.Service != "demo-app" || repo.lastQuery.Limit != "50" {
			t.Fatalf("expected EvidenceRepository service query, got %#v", repo.lastQuery)
		}
	})

	t.Run("service catalog remains available when evidence is unavailable", func(t *testing.T) {
		unavailableAPI := &portalAPI{
			serviceSvc: NewServiceService(writeTestServiceConfig(t), &unavailableServiceEvidenceRepository{}),
		}
		unavailableMux := http.NewServeMux()
		unavailableMux.HandleFunc("/api/v1/services", unavailableAPI.handleServiceList)
		unavailableMux.HandleFunc("/api/v1/services/", unavailableAPI.handleServiceDetail)

		list := callServiceAPI(t, unavailableMux, "/api/v1/services", http.StatusOK)
		item := list["items"].([]interface{})[0].(map[string]interface{})
		if item["latestRelease"] != nil {
			t.Fatalf("expected no latest release when evidence is unavailable, got %#v", item["latestRelease"])
		}

		detail := callServiceAPI(t, unavailableMux, "/api/v1/services/demo-app", http.StatusOK)
		service := detail["service"].(map[string]interface{})
		if service["latestRelease"] != nil {
			t.Fatalf("expected no latest release when evidence is unavailable, got %#v", service["latestRelease"])
		}
	})
}

func writeTestServiceConfig(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	configDir := filepath.Join(repoDir, "configs", "services")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "demo-app.service.yaml"), []byte(testServiceConfig), 0644); err != nil {
		t.Fatalf("write service config: %v", err)
	}

	return repoDir
}

func callServiceAPI(t *testing.T, handler http.Handler, target string, expectedStatus int) map[string]interface{} {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != expectedStatus {
		t.Fatalf("%s: expected HTTP %d, got %d: %s", target, expectedStatus, recorder.Code, recorder.Body.String())
	}

	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

type testServiceEvidenceRepository struct {
	lastQuery EvidenceReleaseListQuery
}

type unavailableServiceEvidenceRepository struct{}

func (repo *unavailableServiceEvidenceRepository) Descriptor() EvidenceRepositoryDescriptor {
	return EvidenceRepositoryDescriptor{}
}

func (repo *unavailableServiceEvidenceRepository) ListReleases(*http.Request, EvidenceReleaseListQuery) (*EvidenceRepositoryResponse, error) {
	return nil, errors.New("evidence store is unavailable")
}
func (repo *unavailableServiceEvidenceRepository) GetRelease(*http.Request, EvidenceReleaseQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *unavailableServiceEvidenceRepository) GetObject(*http.Request, EvidenceObjectQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *unavailableServiceEvidenceRepository) ListArtifacts(*http.Request, EvidenceArtifactListQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *unavailableServiceEvidenceRepository) SearchObjects(*http.Request, EvidenceSearchQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *unavailableServiceEvidenceRepository) GetVerificationSummary(*http.Request, EvidenceVerificationSummaryQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *unavailableServiceEvidenceRepository) GetGraph(*http.Request, EvidenceGraphQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}

func (repo *testServiceEvidenceRepository) Descriptor() EvidenceRepositoryDescriptor {
	return EvidenceRepositoryDescriptor{}
}

func (repo *testServiceEvidenceRepository) ListReleases(_ *http.Request, query EvidenceReleaseListQuery) (*EvidenceRepositoryResponse, error) {
	repo.lastQuery = query
	items := []map[string]interface{}{
		{
			"release_id":     "20260831-000001",
			"release_result": "PASS",
			"generated_at":   "2026-08-31T00:00:01Z",
		},
	}
	if query.Limit == "50" {
		items = append(items, map[string]interface{}{
			"release_id":     "20260830-000001",
			"release_result": "FAIL",
			"generated_at":   "2026-08-30T00:00:01Z",
		})
	}
	body, _ := json.Marshal(map[string]interface{}{"items": items})
	return &EvidenceRepositoryResponse{Body: body}, nil
}

func (repo *testServiceEvidenceRepository) GetRelease(*http.Request, EvidenceReleaseQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *testServiceEvidenceRepository) GetObject(*http.Request, EvidenceObjectQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *testServiceEvidenceRepository) ListArtifacts(*http.Request, EvidenceArtifactListQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *testServiceEvidenceRepository) SearchObjects(*http.Request, EvidenceSearchQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *testServiceEvidenceRepository) GetVerificationSummary(*http.Request, EvidenceVerificationSummaryQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (repo *testServiceEvidenceRepository) GetGraph(*http.Request, EvidenceGraphQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
