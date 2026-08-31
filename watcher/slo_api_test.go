package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testLongWindowSLOConfig = `apiVersion: slo.ssentinel.io/v1alpha1
kind: SLOConfig
metadata:
  name: demo-app-canary-slo
  service: demo-app
  namespace: slo-rollout
  env: dev
spec:
  serviceLevel:
    window: 30d
    availabilityTarget: 99.9
  observability:
    prometheus:
      requestCounter: demo_http_requests_total
      latencyHistogram: demo_http_request_duration_seconds_bucket
      labels:
        namespace: namespace
        status: status
      errorStatusRegex: "5.."
  objectives:
    - id: p95-latency
      name: P95 latency
      type: latency
      percentile: 95
      threshold:
        value: 0.5
        unit: seconds
`

func TestSLOConfigResolution(t *testing.T) {
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	svc := NewSLOService(repoDir, NewServiceService(repoDir, nil), &staticSLOProvider{})

	config, err := svc.ResolveConfig("demo-app-canary-slo")
	if err != nil {
		t.Fatalf("resolve SLO config: %v", err)
	}
	if config.Metadata.Service != "demo-app" || config.Spec.ServiceLevel.Window != "30d" || config.Spec.ServiceLevel.AvailabilityTarget != 99.9 {
		t.Fatalf("unexpected SLO config: %#v", config)
	}
}

func TestPrometheusSLOProviderHealthyAndBreached(t *testing.T) {
	t.Run("healthy SLO", func(t *testing.T) {
		provider := NewPrometheusSLOProvider(newFakePrometheus(t, map[string]string{
			"30d:bad":     "5",
			"30d:total":   "10000",
			"30d:latency": "0.182",
			"1h:bad":      "0.8",
			"1h:total":    "1000",
			"6h:bad":      "0.7",
			"6h:total":    "1000",
			"24h:bad":     "0.6",
			"24h:total":   "1000",
		}).URL)

		status, err := provider.Evaluate(context.Background(), testSLOService(), testSLOConfig(t))
		if err != nil {
			t.Fatalf("evaluate healthy SLO: %v", err)
		}
		if status.Status != SLOStatusHealthy || status.Objectives[0].Current == nil || *status.Objectives[0].Current != 99.95 {
			t.Fatalf("unexpected healthy status: %#v", status)
		}
		if status.ErrorBudget.ConsumedPercent == nil || *status.ErrorBudget.ConsumedPercent < 49.9 || *status.ErrorBudget.ConsumedPercent > 50.1 {
			t.Fatalf("unexpected error budget: %#v", status.ErrorBudget)
		}
	})

	t.Run("breached SLO", func(t *testing.T) {
		provider := NewPrometheusSLOProvider(newFakePrometheus(t, map[string]string{
			"30d:bad":     "2",
			"30d:total":   "1000",
			"30d:latency": "0.8",
			"1h:bad":      "3",
			"1h:total":    "1000",
			"6h:bad":      "2",
			"6h:total":    "1000",
			"24h:bad":     "1.5",
			"24h:total":   "1000",
		}).URL)

		status, err := provider.Evaluate(context.Background(), testSLOService(), testSLOConfig(t))
		if err != nil {
			t.Fatalf("evaluate breached SLO: %v", err)
		}
		if status.Status != SLOStatusBreached || status.ErrorBudget.Status != SLOStatusBreached || status.BurnRate.Status != SLOStatusBreached {
			t.Fatalf("unexpected breached status: %#v", status)
		}
	})
}

func TestSLOBudgetAndBurnRateCalculations(t *testing.T) {
	if got := calculateErrorBudgetConsumedPercent(4, 5000, 99.9); got < 79.9 || got > 80.1 {
		t.Fatalf("expected 80%% error-budget consumption from 4 bad events and 5 allowed bad events, got %v", got)
	}
	if got := calculateBurnRate(0.0005, 99.9); got < 0.49 || got > 0.51 {
		t.Fatalf("expected 0.5x burn rate, got %v", got)
	}
}

func TestPrometheusZeroTrafficReturnsUnknown(t *testing.T) {
	provider := NewPrometheusSLOProvider(newFakePrometheus(t, map[string]string{
		"30d:bad":   "0",
		"30d:total": "0",
	}).URL)

	status, err := provider.Evaluate(context.Background(), testSLOService(), testSLOConfig(t))
	if err != nil {
		t.Fatalf("zero traffic must not fail the SLO endpoint: %v", err)
	}
	if status.Status != SLOStatusUnknown || !strings.Contains(status.Reason, "no request traffic") {
		t.Fatalf("expected zero-traffic UNKNOWN status, got %#v", status)
	}
}

func TestPrometheusUnavailableReturnsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	status, err := NewPrometheusSLOProvider(server.URL).Evaluate(context.Background(), testSLOService(), testSLOConfig(t))
	if err != nil {
		t.Fatalf("unavailable Prometheus must not fail the SLO endpoint: %v", err)
	}
	if status.Status != SLOStatusUnknown || !strings.Contains(status.Reason, "unavailable") {
		t.Fatalf("expected unavailable UNKNOWN status, got %#v", status)
	}
}

func TestServiceSLOAPI(t *testing.T) {
	t.Run("SLO API response", func(t *testing.T) {
		repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
		serviceSvc := NewServiceService(repoDir, nil)
		api := &portalAPI{sloSvc: NewSLOService(repoDir, serviceSvc, &staticSLOProvider{status: ServiceSLOStatus{Service: "demo-app", Status: SLOStatusHealthy, Window: "30d"}})}

		body := callServiceSLOAPI(t, api, "/api/v1/services/demo-app/slo", http.StatusOK)
		slo := body["slo"].(map[string]interface{})
		if body["schemaVersion"] != "service.slo/v1alpha1" || slo["status"] != "HEALTHY" {
			t.Fatalf("unexpected SLO API response: %#v", body)
		}
	})

	t.Run("unknown service returns 404", func(t *testing.T) {
		repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
		api := &portalAPI{sloSvc: NewSLOService(repoDir, NewServiceService(repoDir, nil), &staticSLOProvider{})}

		body := callServiceSLOAPI(t, api, "/api/v1/services/missing/slo", http.StatusNotFound)
		if body["error"] != "service not found" {
			t.Fatalf("unexpected unknown-service response: %#v", body)
		}
	})

	t.Run("service without SLO is unknown", func(t *testing.T) {
		serviceConfigWithoutSLO := strings.Replace(testServiceConfig, "reliability:\n  sloRef: demo-app-canary-slo\n", "reliability: {}\n", 1)
		repoDir := writeSLOTestRepo(t, serviceConfigWithoutSLO, testLongWindowSLOConfig)
		api := &portalAPI{sloSvc: NewSLOService(repoDir, NewServiceService(repoDir, nil), &staticSLOProvider{})}

		body := callServiceSLOAPI(t, api, "/api/v1/services/demo-app/slo", http.StatusOK)
		slo := body["slo"].(map[string]interface{})
		if slo["status"] != "UNKNOWN" || !strings.Contains(slo["reason"].(string), "not configured") {
			t.Fatalf("unexpected unconfigured SLO response: %#v", body)
		}
	})
}

func writeSLOTestRepo(t *testing.T, serviceConfig, sloConfig string) string {
	t.Helper()
	repoDir := t.TempDir()
	configDir := filepath.Join(repoDir, "configs", "services")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "demo-app.service.yaml"), []byte(serviceConfig), 0644); err != nil {
		t.Fatalf("write service config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "demo-app.slo.yaml"), []byte(sloConfig), 0644); err != nil {
		t.Fatalf("write SLO config: %v", err)
	}
	return repoDir
}

func newFakePrometheus(t *testing.T, values map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		kind := "total"
		if strings.Contains(query, "histogram_quantile") {
			kind = "latency"
		} else if strings.Contains(query, "status=~") {
			kind = "bad"
		}
		window := ""
		for _, candidate := range []string{"30d", "24h", "6h", "1h"} {
			if strings.Contains(query, "["+candidate+"]") {
				window = candidate
				break
			}
		}
		value, ok := values[window+":"+kind]
		if !ok {
			http.Error(w, "missing fake Prometheus sample", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result":     []interface{}{map[string]interface{}{"metric": map[string]string{}, "value": []interface{}{float64(1), value}}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func testSLOService() Service {
	return Service{Metadata: ServiceMetadata{Name: "demo-app"}, Runtime: ServiceRuntimeRef{Namespace: "slo-rollout"}}
}

func testSLOConfig(t *testing.T) ServiceSLOConfig {
	t.Helper()
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	config, err := NewSLOService(repoDir, NewServiceService(repoDir, nil), &staticSLOProvider{}).ResolveConfig("demo-app-canary-slo")
	if err != nil {
		t.Fatalf("load test SLO config: %v", err)
	}
	return config
}

func callServiceSLOAPI(t *testing.T, api *portalAPI, target string, expectedStatus int) map[string]interface{} {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.handleServiceDetail(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != expectedStatus {
		t.Fatalf("%s: expected HTTP %d, got %d: %s", target, expectedStatus, recorder.Code, recorder.Body.String())
	}
	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode SLO API response: %v", err)
	}
	return body
}

type staticSLOProvider struct {
	status ServiceSLOStatus
}

func (provider *staticSLOProvider) Evaluate(_ context.Context, service Service, _ ServiceSLOConfig) (ServiceSLOStatus, error) {
	if provider.status.Service == "" {
		provider.status.Service = service.Metadata.Name
	}
	if provider.status.Status == "" {
		provider.status.Status = SLOStatusHealthy
	}
	return provider.status, nil
}
