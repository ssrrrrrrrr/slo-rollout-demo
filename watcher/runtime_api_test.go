package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func TestKubernetesRuntimeProviderStatus(t *testing.T) {
	t.Run("healthy runtime", func(t *testing.T) {
		provider := fakeRuntimeProvider(t,
			testRuntimeRollout(3, 3, 3, 3, "Healthy"),
			testRuntimePod("demo-app-a", true, []int64{0}),
			testRuntimePod("demo-app-b", true, []int64{1}),
			testRuntimePod("demo-app-c", true, []int64{0}),
		)

		snapshot, err := provider.Snapshot(context.Background(), testRuntimeService())
		if err != nil {
			t.Fatalf("snapshot healthy runtime: %v", err)
		}
		if snapshot.Status != RuntimeStatusHealthy || snapshot.Workload.ReadyReplicas != 3 || snapshot.PrimaryImage != "demo-app:v1.8.2" {
			t.Fatalf("unexpected healthy snapshot: %#v", snapshot)
		}
	})

	t.Run("degraded replicas", func(t *testing.T) {
		provider := fakeRuntimeProvider(t,
			testRuntimeRollout(3, 2, 2, 2, "Progressing"),
			testRuntimePod("demo-app-a", true, []int64{0}),
			testRuntimePod("demo-app-b", false, []int64{0}),
		)

		snapshot, err := provider.Snapshot(context.Background(), testRuntimeService())
		if err != nil || snapshot.Status != RuntimeStatusDegraded {
			t.Fatalf("expected DEGRADED snapshot, got %#v err=%v", snapshot, err)
		}
	})

	t.Run("unhealthy zero-ready runtime", func(t *testing.T) {
		provider := fakeRuntimeProvider(t,
			testRuntimeRollout(3, 0, 0, 1, "Progressing"),
			testRuntimePod("demo-app-a", false, []int64{0}),
			testRuntimePod("demo-app-b", false, []int64{0}),
		)

		snapshot, err := provider.Snapshot(context.Background(), testRuntimeService())
		if err != nil || snapshot.Status != RuntimeStatusUnhealthy {
			t.Fatalf("expected UNHEALTHY snapshot, got %#v err=%v", snapshot, err)
		}
	})

	t.Run("unhealthy when no pods and no replicas are ready or available", func(t *testing.T) {
		provider := fakeRuntimeProvider(t, testRuntimeRollout(3, 0, 0, 0, "Progressing"))

		snapshot, err := provider.Snapshot(context.Background(), testRuntimeService())
		if err != nil || snapshot.Status != RuntimeStatusUnhealthy {
			t.Fatalf("expected empty pod discovery with zero replicas to be UNHEALTHY, got %#v err=%v", snapshot, err)
		}
	})

	t.Run("unknown when pod discovery conflicts with workload replica status", func(t *testing.T) {
		provider := fakeRuntimeProvider(t, testRuntimeRollout(3, 3, 3, 3, "Healthy"))

		snapshot, err := provider.Snapshot(context.Background(), testRuntimeService())
		if err != nil || snapshot.Status != RuntimeStatusUnknown || !strings.Contains(snapshot.Reason, "pod discovery found no pods") {
			t.Fatalf("expected inconsistent pod discovery to be UNKNOWN, got %#v err=%v", snapshot, err)
		}
	})
}

func TestRuntimePodReadinessAndRestartAggregation(t *testing.T) {
	provider := fakeRuntimeProvider(t,
		testRuntimeRollout(1, 1, 1, 1, "Healthy"),
		testRuntimePod("demo-app-a", true, []int64{2, 3}),
	)

	snapshot, err := provider.Snapshot(context.Background(), testRuntimeService())
	if err != nil {
		t.Fatalf("snapshot runtime: %v", err)
	}
	if len(snapshot.Pods) != 1 || !snapshot.Pods[0].Ready || snapshot.Pods[0].RestartCount != 5 {
		t.Fatalf("expected container-readiness and restart aggregation, got %#v", snapshot.Pods)
	}
	if snapshot.Status != RuntimeStatusDegraded {
		t.Fatalf("expected high restart count to degrade runtime, got %s", snapshot.Status)
	}
}

func TestRuntimeServiceUnknownStates(t *testing.T) {
	t.Run("Kubernetes unavailable", func(t *testing.T) {
		svc := runtimeServiceForTest(t, testServiceConfig, &KubernetesRuntimeProvider{initErr: errors.New("connection refused")})
		snapshot, err := svc.Snapshot(context.Background(), "demo-app")
		if err != nil || snapshot.Status != RuntimeStatusUnknown || !strings.Contains(snapshot.Reason, "Kubernetes runtime data unavailable") {
			t.Fatalf("expected Kubernetes UNKNOWN status, got %#v err=%v", snapshot, err)
		}
	})

	t.Run("runtime not configured", func(t *testing.T) {
		serviceWithoutRuntime := strings.Replace(testServiceConfig, "runtime:\n  namespace: slo-rollout\n  workload:\n    kind: Rollout\n    name: demo-app\n", "runtime: {}\n", 1)
		svc := runtimeServiceForTest(t, serviceWithoutRuntime, &staticRuntimeProvider{})
		snapshot, err := svc.Snapshot(context.Background(), "demo-app")
		if err != nil || snapshot.Status != RuntimeStatusUnknown || !strings.Contains(snapshot.Reason, "not configured") {
			t.Fatalf("expected unconfigured Runtime UNKNOWN status, got %#v err=%v", snapshot, err)
		}
	})

	t.Run("Rollout not found", func(t *testing.T) {
		svc := runtimeServiceForTest(t, testServiceConfig, fakeRuntimeProvider(t))
		snapshot, err := svc.Snapshot(context.Background(), "demo-app")
		if err != nil || snapshot.Status != RuntimeStatusUnknown || !strings.Contains(snapshot.Reason, "get Rollout") {
			t.Fatalf("expected missing Rollout UNKNOWN status, got %#v err=%v", snapshot, err)
		}
	})
}

func TestRuntimeAPI(t *testing.T) {
	repoDir := writeSLOTestRepo(t, testServiceConfig, testLongWindowSLOConfig)
	serviceSvc := NewServiceService(repoDir, nil)
	api := &portalAPI{runtimeSvc: NewRuntimeService(serviceSvc, &staticRuntimeProvider{snapshot: RuntimeSnapshot{
		Service:    "demo-app",
		Status:     RuntimeStatusHealthy,
		Namespace:  "slo-rollout",
		ObservedAt: time.Now().Format(time.RFC3339),
	}})}

	t.Run("API response", func(t *testing.T) {
		body := callServiceRuntimeAPI(t, api, "/api/v1/services/demo-app/runtime", http.StatusOK)
		runtime := body["runtime"].(map[string]interface{})
		if body["schemaVersion"] != "service.runtime/v1alpha1" || runtime["status"] != "HEALTHY" {
			t.Fatalf("unexpected runtime API response: %#v", body)
		}
	})

	t.Run("unknown service returns 404", func(t *testing.T) {
		body := callServiceRuntimeAPI(t, api, "/api/v1/services/missing/runtime", http.StatusNotFound)
		if body["error"] != "service not found" {
			t.Fatalf("unexpected unknown-service response: %#v", body)
		}
	})
}

func fakeRuntimeProvider(t *testing.T, objects ...*unstructured.Unstructured) *KubernetesRuntimeProvider {
	t.Helper()
	var rollout *unstructured.Unstructured
	pods := []unstructured.Unstructured{}
	for _, object := range objects {
		if object.GetKind() == "Rollout" {
			rollout = object
			continue
		}
		if object.GetKind() == "Pod" {
			pods = append(pods, *object)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/argoproj.io/v1alpha1/namespaces/slo-rollout/rollouts/demo-app":
			if rollout == nil {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(rollout)
		case "/api/v1/namespaces/slo-rollout/pods":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "PodList",
				"items":      pods,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("create fake Kubernetes dynamic client: %v", err)
	}
	return NewKubernetesRuntimeProviderForClient(client)
}

func runtimeServiceForTest(t *testing.T, serviceConfig string, provider RuntimeProvider) *RuntimeService {
	t.Helper()
	repoDir := writeSLOTestRepo(t, serviceConfig, testLongWindowSLOConfig)
	return NewRuntimeService(NewServiceService(repoDir, nil), provider)
}

func testRuntimeService() Service {
	return Service{
		Metadata: ServiceMetadata{Name: "demo-app"},
		Runtime: ServiceRuntimeRef{
			Namespace: "slo-rollout",
			Workload:  ServiceWorkloadRef{Kind: "Rollout", Name: "demo-app"},
		},
	}
}

func testRuntimeRollout(desired, ready, available, updated int64, phase string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]interface{}{
			"name":      "demo-app",
			"namespace": "slo-rollout",
		},
		"spec": map[string]interface{}{
			"replicas": desired,
			"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "demo-app"}},
			"template": map[string]interface{}{
				"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "demo-app", "image": "demo-app:v1.8.2"}}},
			},
		},
		"status": map[string]interface{}{
			"phase":             phase,
			"currentPodHash":    "demo-app-8",
			"readyReplicas":     ready,
			"availableReplicas": available,
			"updatedReplicas":   updated,
		},
	}}
}

func testRuntimePod(name string, ready bool, restarts []int64) *unstructured.Unstructured {
	containerStatuses := make([]interface{}, 0, len(restarts))
	containers := make([]interface{}, 0, len(restarts))
	for index, restartCount := range restarts {
		containerName := "demo-app"
		if index > 0 {
			containerName = "sidecar"
		}
		containerStatuses = append(containerStatuses, map[string]interface{}{
			"name":         containerName,
			"ready":        ready,
			"restartCount": restartCount,
			"image":        "demo-app:v1.8.2",
		})
		containers = append(containers, map[string]interface{}{"name": containerName, "image": "demo-app:v1.8.2"})
	}
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "slo-rollout",
			"labels":    map[string]interface{}{"app": "demo-app"},
		},
		"spec": map[string]interface{}{
			"nodeName":   "k8s-node1",
			"containers": containers,
		},
		"status": map[string]interface{}{
			"phase":             "Running",
			"containerStatuses": containerStatuses,
		},
	}}
	pod.SetCreationTimestamp(metav1.NewTime(time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)))
	return pod
}

func callServiceRuntimeAPI(t *testing.T, api *portalAPI, target string, expectedStatus int) map[string]interface{} {
	t.Helper()
	recorder := httptest.NewRecorder()
	api.handleServiceDetail(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != expectedStatus {
		t.Fatalf("%s: expected HTTP %d, got %d: %s", target, expectedStatus, recorder.Code, recorder.Body.String())
	}
	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode runtime API response: %v", err)
	}
	return body
}

type staticRuntimeProvider struct {
	snapshot RuntimeSnapshot
}

func (provider *staticRuntimeProvider) Snapshot(_ context.Context, service Service) (RuntimeSnapshot, error) {
	if provider.snapshot.Service == "" {
		provider.snapshot.Service = service.Metadata.Name
	}
	if provider.snapshot.Status == "" {
		provider.snapshot.Status = RuntimeStatusHealthy
	}
	return provider.snapshot, nil
}
