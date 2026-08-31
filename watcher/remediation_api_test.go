package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRemediationPlanEligibility(t *testing.T) {
	t.Run("actionable release projects existing recommendation", func(t *testing.T) {
		h := newRemediationHarness(t)
		plan := h.plan(t)
		if plan.Status != RemediationPlanActionable || plan.Recommendation.Action != "ROLLBACK" || plan.Policy.Decision != "ALLOW" || plan.Eligibility.Eligible {
			t.Fatalf("unexpected plan: %#v", plan)
		}
	})
	t.Run("no related release is not actionable", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.repository.present = false
		plan := h.plan(t)
		if plan.Status != RemediationPlanNotActionable || plan.Recommendation.Action != "NONE" {
			t.Fatalf("unexpected plan: %#v", plan)
		}
	})
	t.Run("stale release is not actionable", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.repository.timestamp = time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
		plan := h.plan(t)
		if plan.Status != RemediationPlanNotActionable {
			t.Fatalf("unexpected stale plan: %#v", plan)
		}
	})
	t.Run("no recommendation is not actionable", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.repository.action = ""
		plan := h.plan(t)
		if plan.Status != RemediationPlanNotActionable {
			t.Fatalf("unexpected no-recommendation plan: %#v", plan)
		}
	})
	t.Run("policy deny blocks remediation", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.repository.policy = "DENY"
		plan := h.plan(t)
		if plan.Status != RemediationPlanBlocked || plan.Eligibility.Eligible {
			t.Fatalf("unexpected deny plan: %#v", plan)
		}
	})
	t.Run("approval state controls eligibility", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.repository.policy = "ALLOW_WITH_APPROVAL"
		h.repository.requiresApproval = true
		blocked := h.plan(t)
		if blocked.Eligibility.Eligible {
			t.Fatalf("approval should block: %#v", blocked)
		}
		h.repository.approved = true
		eligible := h.plan(t)
		if !eligible.Eligibility.Eligible {
			t.Fatalf("approved plan should be eligible: %#v", eligible)
		}
	})
}

func TestRemediationPreviewExecuteAndVerification(t *testing.T) {
	t.Run("preview never invokes execution", func(t *testing.T) {
		h := newRemediationHarness(t)
		incident := h.incident(t)
		if _, err := h.service.Preview(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID); err != nil || h.adapter.calls != 0 {
			t.Fatalf("preview must not execute: err=%v calls=%d", err, h.adapter.calls)
		}
	})
	t.Run("disabled gates block execute and invalid action is rejected", func(t *testing.T) {
		h := newRemediationHarness(t)
		incident := h.incident(t)
		if _, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK"); remediationErrorStatus(err) != 409 {
			t.Fatalf("expected gate block, got %v", err)
		}
		if h.adapter.calls != 0 {
			t.Fatalf("global gate must not invoke adapter: %d", h.adapter.calls)
		}
		h.enableRuntimeGates(t)
		if _, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "PAUSE"); remediationErrorStatus(err) != 400 {
			t.Fatalf("expected invalid action rejection, got %v", err)
		}
	})
	t.Run("action gate, approval, and policy blocks never invoke adapter", func(t *testing.T) {
		h := newRemediationHarness(t)
		incident := h.incident(t)
		t.Setenv("S_SENTINEL_RUNTIME_EXECUTION_ENABLED", "true")
		t.Setenv("S_SENTINEL_RUNTIME_ACTION_APPROVED", "true")
		t.Setenv("S_SENTINEL_RUNTIME_ROLLBACK_EXECUTE", "true")
		if _, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK"); remediationErrorStatus(err) != 409 || h.adapter.calls != 0 {
			t.Fatalf("action gate must block adapter: err=%v calls=%d", err, h.adapter.calls)
		}
		h = newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.repository.policy = "ALLOW_WITH_APPROVAL"
		h.repository.requiresApproval = true
		incident = h.incident(t)
		if _, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK"); remediationErrorStatus(err) != 409 || h.adapter.calls != 0 {
			t.Fatalf("approval must block adapter: err=%v calls=%d", err, h.adapter.calls)
		}
		h = newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.repository.policy = "DENY"
		incident = h.incident(t)
		if _, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK"); remediationErrorStatus(err) != 409 || h.adapter.calls != 0 {
			t.Fatalf("policy must block adapter: err=%v calls=%d", err, h.adapter.calls)
		}
	})
	t.Run("successful execution is idempotent and verifies recovering", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.runtime.status = RuntimeStatusHealthy
		h.slo.status = SLOStatusBreached
		incident := h.incident(t)
		plan, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK")
		if err != nil || plan.Execution == nil || plan.Execution.Status != "SUCCEEDED" || plan.Verification == nil || plan.Verification.Status != RemediationVerificationRecovering {
			t.Fatalf("unexpected execution: %#v err=%v", plan, err)
		}
		duplicate, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK")
		if err != nil || duplicate.Execution == nil || !duplicate.Execution.Idempotent || h.adapter.calls != 1 {
			t.Fatalf("expected idempotent execute: %#v err=%v calls=%d", duplicate, err, h.adapter.calls)
		}
	})
	t.Run("healthy runtime and recovered SLO is recovered", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.runtime.status = RuntimeStatusHealthy
		h.slo.status = SLOStatusHealthy
		incident := h.incident(t)
		plan, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK")
		if err != nil || plan.Verification == nil || plan.Verification.Status != RemediationVerificationRecovered {
			t.Fatalf("expected recovered verification: %#v err=%v", plan, err)
		}
	})
	t.Run("execution failure is projected", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.adapter.executeErr = errors.New("runtime action pipeline failed")
		incident := h.incident(t)
		plan, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK")
		if err != nil || plan.Execution == nil || plan.Execution.Status != "FAILED" || plan.Verification == nil || plan.Verification.Status != RemediationVerificationFailed {
			t.Fatalf("unexpected failed execution: %#v err=%v", plan, err)
		}
	})
	t.Run("runtime action result is projected and its verification is consumed", func(t *testing.T) {
		h := newRemediationHarness(t)
		h.enableRuntimeGates(t)
		h.runtime.status = RuntimeStatusHealthy
		h.slo.status = SLOStatusBreached
		h.adapter.result = RuntimeActionExecutionProjection{ResultID: "raer-release-123", Status: "SUCCEEDED", StartedAt: "2026-01-01T00:00:00Z", FinishedAt: "2026-01-01T00:00:02Z", Reason: "existing result", Target: RemediationTarget{ReleaseID: "release-123", Namespace: "slo-rollout", Workload: "demo-app"}, PostState: map[string]interface{}{"phase": "Healthy"}, ActionVerified: true}
		incident := h.incident(t)
		plan, err := h.service.Execute(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil), incident.ID, "ROLLBACK")
		if err != nil || plan.Execution == nil || plan.Execution.ResultID != "raer-release-123" || plan.Execution.Target.Workload != "demo-app" || plan.Execution.PostState["phase"] != "Healthy" || plan.Verification == nil || plan.Verification.Status != RemediationVerificationRecovering {
			t.Fatalf("runtime result was not projected: %#v err=%v", plan, err)
		}
	})
}

func TestRemediationAPI(t *testing.T) {
	h := newRemediationHarness(t)
	api := &portalAPI{remediationSvc: h.service}
	incident := h.incident(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/incidents/", api.handleIncidentDetail)
	plan := callRemediationAPI(t, mux, http.MethodGet, "/api/v1/incidents/"+incident.ID+"/remediation", nil, http.StatusOK)
	if plan["schemaVersion"] != "incident.remediation/v1alpha1" {
		t.Fatalf("unexpected remediation API: %#v", plan)
	}
	preview := callRemediationAPI(t, mux, http.MethodPost, "/api/v1/incidents/"+incident.ID+"/remediation/preview", nil, http.StatusOK)
	if preview["remediation"] == nil {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	verification := callRemediationAPI(t, mux, http.MethodGet, "/api/v1/incidents/"+incident.ID+"/remediation/verification", nil, http.StatusOK)
	if verification["verification"].(map[string]interface{})["status"] != "PENDING" {
		t.Fatalf("unexpected verification: %#v", verification)
	}
	h.enableRuntimeGates(t)
	executed := callRemediationAPI(t, mux, http.MethodPost, "/api/v1/incidents/"+incident.ID+"/remediation/execute", []byte(`{"action":"ROLLBACK"}`), http.StatusOK)
	if executed["remediation"].(map[string]interface{})["execution"].(map[string]interface{})["status"] != "SUCCEEDED" {
		t.Fatalf("unexpected execution API: %#v", executed)
	}
	unknown := callRemediationAPI(t, mux, http.MethodGet, "/api/v1/incidents/INC-missing/remediation", nil, http.StatusNotFound)
	if unknown["error"] != "incident not found" {
		t.Fatalf("unexpected unknown incident: %#v", unknown)
	}
}

func TestRuntimeActionPipelineResultProjection(t *testing.T) {
	reportDir := t.TempDir()
	adapter := NewRuntimeActionPipelineAdapter(t.TempDir(), reportDir)
	result := map[string]interface{}{
		"runtimeActionExecutionResultId": "raer-release-123",
		"generatedAt":                    time.Now().UTC().Format(time.RFC3339),
		"release":                        map[string]interface{}{"releaseId": "release-123"},
		"target":                         map[string]interface{}{"cluster": "local-dev", "namespace": "slo-rollout", "rolloutName": "demo-app"},
		"action":                         map[string]interface{}{"requestedAction": "ROLLBACK_ROLLOUT", "commandStartedAt": "2026-01-01T00:00:00Z", "commandFinishedAt": "2026-01-01T00:00:03Z"},
		"result":                         map[string]interface{}{"executionStatus": "SUCCEEDED", "summary": "runtime action completed"},
		"postActionVerification":         map[string]interface{}{"verificationStatus": "VERIFIED"},
		"verificationSummary":            map[string]interface{}{"verified": true},
		"afterSnapshot":                  map[string]interface{}{"phase": "Healthy", "readyReplicas": float64(2)},
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "runtime-action-execution-result-release-123.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err := adapter.loadResult(RemediationExecutionRequest{ReleaseID: "release-123", Action: "ROLLBACK"}, time.Now().Add(-time.Minute))
	if err != nil || projection.Status != "SUCCEEDED" || !projection.ActionVerified || projection.ResultID != "raer-release-123" || projection.Target.Namespace != "slo-rollout" || projection.PostState["phase"] != "Healthy" {
		t.Fatalf("unexpected runtime action projection: %#v err=%v", projection, err)
	}
}

type remediationHarness struct {
	service     *RemediationService
	incidentSvc *IncidentService
	repository  *remediationTestRepository
	slo         *remediationSLOProvider
	runtime     *remediationRuntimeProvider
	adapter     *remediationExecutionAdapter
}

func newRemediationHarness(t *testing.T) *remediationHarness {
	t.Helper()
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	repository := &remediationTestRepository{present: true, timestamp: time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339), action: "ROLLBACK", policy: "ALLOW"}
	serviceSvc := NewServiceService(repoDir, repository)
	slo := &remediationSLOProvider{status: SLOStatusBreached}
	runtime := &remediationRuntimeProvider{status: RuntimeStatusDegraded}
	adapter := &remediationExecutionAdapter{}
	sloSvc := NewSLOService(repoDir, serviceSvc, slo)
	runtimeSvc := NewRuntimeService(serviceSvc, runtime)
	incidentSvc := NewIncidentService(serviceSvc, sloSvc, runtimeSvc, NewReliabilityIncidentDetector())
	return &remediationHarness{service: NewRemediationService(incidentSvc, sloSvc, runtimeSvc, repository, adapter), incidentSvc: incidentSvc, repository: repository, slo: slo, runtime: runtime, adapter: adapter}
}

func (h *remediationHarness) incident(t *testing.T) *ReliabilityIncident {
	t.Helper()
	incident, err := h.incidentSvc.ActiveForService(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), "demo-app")
	if err != nil || incident == nil {
		t.Fatalf("create test incident: %#v err=%v", incident, err)
	}
	return incident
}
func (h *remediationHarness) plan(t *testing.T) RemediationPlan {
	t.Helper()
	incident := h.incident(t)
	plan, err := h.service.Plan(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), incident.ID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}
func (h *remediationHarness) enableRuntimeGates(t *testing.T) {
	t.Helper()
	t.Setenv("S_SENTINEL_RUNTIME_EXECUTION_ENABLED", "true")
	t.Setenv("S_SENTINEL_RUNTIME_ACTION_APPROVED", "true")
	t.Setenv("S_SENTINEL_ALLOW_RUNTIME_ROLLBACK", "true")
	t.Setenv("S_SENTINEL_RUNTIME_ROLLBACK_EXECUTE", "true")
}

type remediationTestRepository struct {
	present                    bool
	timestamp, action, policy  string
	requiresApproval, approved bool
}

func (*remediationTestRepository) Descriptor() EvidenceRepositoryDescriptor {
	return EvidenceRepositoryDescriptor{}
}
func (repo *remediationTestRepository) ListReleases(_ *http.Request, _ EvidenceReleaseListQuery) (*EvidenceRepositoryResponse, error) {
	items := []map[string]interface{}{}
	if repo.present {
		items = append(items, map[string]interface{}{"release_id": "release-123", "release_result": "FAILED", "generated_at": repo.timestamp, "policy_decision": repo.policy, "final_action": repo.action})
	}
	body, _ := json.Marshal(map[string]interface{}{"items": items})
	return &EvidenceRepositoryResponse{Body: body}, nil
}
func (repo *remediationTestRepository) GetRelease(_ *http.Request, _ EvidenceReleaseQuery) (*EvidenceRepositoryResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{"release": map[string]interface{}{"policy_decision": repo.policy, "final_action": repo.action, "requires_human_approval": repo.requiresApproval, "approved": repo.approved}})
	return &EvidenceRepositoryResponse{Body: body}, nil
}
func (*remediationTestRepository) GetObject(*http.Request, EvidenceObjectQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*remediationTestRepository) ListArtifacts(*http.Request, EvidenceArtifactListQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*remediationTestRepository) SearchObjects(*http.Request, EvidenceSearchQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*remediationTestRepository) GetVerificationSummary(*http.Request, EvidenceVerificationSummaryQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}
func (*remediationTestRepository) GetGraph(*http.Request, EvidenceGraphQuery) (*EvidenceRepositoryResponse, error) {
	return nil, nil
}

type remediationSLOProvider struct{ status SLOStatus }

func (provider *remediationSLOProvider) Evaluate(_ context.Context, service Service, _ ServiceSLOConfig) (ServiceSLOStatus, error) {
	return ServiceSLOStatus{Service: service.Metadata.Name, Status: provider.status, EvaluatedAt: time.Now().Format(time.RFC3339), BurnRate: BurnRateStatus{Status: provider.status}, ErrorBudget: ErrorBudgetStatus{Status: provider.status}}, nil
}

type remediationRuntimeProvider struct{ status RuntimeStatus }

func (provider *remediationRuntimeProvider) Snapshot(_ context.Context, service Service) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{Service: service.Metadata.Name, Status: provider.status, ObservedAt: time.Now().Format(time.RFC3339), Pods: []RuntimePodStatus{}}, nil
}

type remediationExecutionAdapter struct {
	mu           sync.Mutex
	calls        int
	availableErr error
	executeErr   error
	result       RuntimeActionExecutionProjection
}

func (adapter *remediationExecutionAdapter) Available(RemediationExecutionRequest) error {
	return adapter.availableErr
}
func (adapter *remediationExecutionAdapter) Execute(_ context.Context, request RemediationExecutionRequest) (RuntimeActionExecutionProjection, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.calls++
	if adapter.executeErr != nil {
		return RuntimeActionExecutionProjection{}, adapter.executeErr
	}
	result := adapter.result
	if result.Status == "" {
		result.Status = "SUCCEEDED"
	}
	if result.Action == "" {
		result.Action = request.Action
	}
	if result.Target.ReleaseID == "" {
		result.Target.ReleaseID = request.ReleaseID
	}
	if !result.ActionVerified {
		result.ActionVerified = true
	}
	return result, nil
}

func remediationErrorStatus(err error) int {
	var requestError *RemediationRequestError
	if errors.As(err, &requestError) {
		return requestError.StatusCode
	}
	return 0
}
func callRemediationAPI(t *testing.T, handler http.Handler, method, target string, body []byte, expectedStatus int) map[string]interface{} {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	if body != nil {
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != expectedStatus {
		t.Fatalf("%s %s: expected %d got %d: %s", method, target, expectedStatus, recorder.Code, recorder.Body.String())
	}
	decoded := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return decoded
}
