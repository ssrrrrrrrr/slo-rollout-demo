package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var runtimePodGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

const degradedRestartCount = int64(5)

type KubernetesRuntimeProvider struct {
	client  dynamic.Interface
	initErr error
}

func NewKubernetesRuntimeProvider() *KubernetesRuntimeProvider {
	kubeConfig, err := buildKubeConfig()
	if err != nil {
		return &KubernetesRuntimeProvider{initErr: err}
	}

	client, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return &KubernetesRuntimeProvider{initErr: err}
	}

	return NewKubernetesRuntimeProviderForClient(client)
}

func NewKubernetesRuntimeProviderForClient(client dynamic.Interface) *KubernetesRuntimeProvider {
	return &KubernetesRuntimeProvider{client: client}
}

func (provider *KubernetesRuntimeProvider) Snapshot(ctx context.Context, service Service) (RuntimeSnapshot, error) {
	if provider.initErr != nil {
		return RuntimeSnapshot{}, provider.initErr
	}
	if provider.client == nil {
		return RuntimeSnapshot{}, fmt.Errorf("Kubernetes dynamic client is unavailable")
	}
	if service.Runtime.Workload.Kind != "Rollout" {
		return RuntimeSnapshot{}, fmt.Errorf("unsupported runtime workload kind %q", service.Runtime.Workload.Kind)
	}

	namespace := service.Runtime.Namespace
	rollout, err := provider.client.Resource(rolloutGVR).Namespace(namespace).Get(ctx, service.Runtime.Workload.Name, metav1.GetOptions{})
	if err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("get Rollout %s/%s: %w", namespace, service.Runtime.Workload.Name, err)
	}

	selector, err := rolloutSelector(rollout)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	podList, err := provider.client.Resource(runtimePodGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("list pods for Rollout %s/%s: %w", namespace, rollout.GetName(), err)
	}

	pods := make([]RuntimePodStatus, 0, len(podList.Items))
	for index := range podList.Items {
		pods = append(pods, runtimePodStatus(&podList.Items[index]))
	}

	images := rolloutImages(rollout)
	snapshot := RuntimeSnapshot{
		Service:   service.Metadata.Name,
		Namespace: namespace,
		Workload: RuntimeWorkloadStatus{
			Kind:              service.Runtime.Workload.Kind,
			Name:              rollout.GetName(),
			Phase:             getString(rollout, "status", "phase"),
			Revision:          rolloutRevision(rollout),
			DesiredReplicas:   getInt64(rollout, "spec", "replicas"),
			ReadyReplicas:     getInt64(rollout, "status", "readyReplicas"),
			AvailableReplicas: getInt64(rollout, "status", "availableReplicas"),
			UpdatedReplicas:   getInt64(rollout, "status", "updatedReplicas"),
		},
		Images:     images,
		Pods:       pods,
		ObservedAt: time.Now().Format(time.RFC3339),
	}
	if len(images) > 0 {
		snapshot.PrimaryImage = images[0]
	}

	snapshot.Status, snapshot.Reason = evaluateRuntimeStatus(snapshot)
	return snapshot, nil
}

func rolloutSelector(rollout *unstructured.Unstructured) (string, error) {
	matchLabels, found, err := unstructured.NestedStringMap(rollout.Object, "spec", "selector", "matchLabels")
	if err != nil || !found || len(matchLabels) == 0 {
		return "", fmt.Errorf("Rollout %s has no usable spec.selector.matchLabels", rollout.GetName())
	}

	return labels.Set(matchLabels).String(), nil
}

func rolloutImages(rollout *unstructured.Unstructured) []string {
	containers, found, _ := unstructured.NestedSlice(rollout.Object, "spec", "template", "spec", "containers")
	if !found {
		return []string{}
	}

	images := make([]string, 0, len(containers))
	for _, item := range containers {
		container, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if image, ok := container["image"].(string); ok && strings.TrimSpace(image) != "" {
			images = append(images, image)
		}
	}

	return images
}

func rolloutRevision(rollout *unstructured.Unstructured) string {
	for _, fields := range [][]string{{"status", "currentRevision"}, {"status", "currentPodHash"}, {"status", "stableRS"}} {
		if value := getString(rollout, fields...); value != "" {
			return value
		}
	}
	return ""
}

func runtimePodStatus(pod *unstructured.Unstructured) RuntimePodStatus {
	containerStatuses, found, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	containers := make([]RuntimeContainerStatus, 0, len(containerStatuses))
	ready := found && len(containerStatuses) > 0
	var restartCount int64

	for _, item := range containerStatuses {
		container, ok := item.(map[string]interface{})
		if !ok {
			ready = false
			continue
		}
		containerReady, _ := container["ready"].(bool)
		containerRestartCount := int64FromInterface(container["restartCount"])
		containerImage, _ := container["image"].(string)
		containerName, _ := container["name"].(string)

		containers = append(containers, RuntimeContainerStatus{
			Name:         containerName,
			Ready:        containerReady,
			RestartCount: containerRestartCount,
			Image:        containerImage,
		})
		restartCount += containerRestartCount
		if !containerReady {
			ready = false
		}
	}

	image := ""
	if len(containers) > 0 {
		image = containers[0].Image
	}
	if image == "" {
		image = firstPodSpecImage(pod)
	}

	return RuntimePodStatus{
		Name:         pod.GetName(),
		Phase:        getString(pod, "status", "phase"),
		Ready:        ready,
		RestartCount: restartCount,
		Node:         getString(pod, "spec", "nodeName"),
		Image:        image,
		CreatedAt:    pod.GetCreationTimestamp().Time.Format(time.RFC3339),
		Containers:   containers,
	}
}

func firstPodSpecImage(pod *unstructured.Unstructured) string {
	containers, found, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	if !found {
		return ""
	}
	for _, item := range containers {
		container, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if image, ok := container["image"].(string); ok {
			return image
		}
	}
	return ""
}

func evaluateRuntimeStatus(snapshot RuntimeSnapshot) (RuntimeStatus, string) {
	workload := snapshot.Workload
	if workload.DesiredReplicas <= 0 {
		return RuntimeStatusUnknown, "workload has no desired replicas"
	}
	if isFailedRolloutPhase(workload.Phase) {
		return RuntimeStatusUnhealthy, "workload phase is " + workload.Phase
	}
	if len(snapshot.Pods) == 0 {
		if workload.ReadyReplicas == 0 && workload.AvailableReplicas == 0 {
			return RuntimeStatusUnhealthy, "no workload replicas are ready or available"
		}
		return RuntimeStatusUnknown, "workload status reports ready or available replicas, but pod discovery found no pods for the Rollout selector"
	}
	if workload.ReadyReplicas == 0 || allRuntimePodsUnavailable(snapshot.Pods) {
		return RuntimeStatusUnhealthy, "no workload replicas are ready"
	}
	if workload.ReadyReplicas < workload.DesiredReplicas || workload.AvailableReplicas < workload.DesiredReplicas {
		return RuntimeStatusDegraded, "ready or available replicas are below desired replicas"
	}
	if hasNonReadyRuntimePod(snapshot.Pods) {
		return RuntimeStatusDegraded, "one or more workload pods are not ready"
	}
	if hasHighRestartCount(snapshot.Pods) {
		return RuntimeStatusDegraded, "one or more workload pods have a high restart count"
	}

	return RuntimeStatusHealthy, "all workload replicas and pods are ready"
}

func isFailedRolloutPhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "degraded", "failed", "error":
		return true
	default:
		return false
	}
}

func allRuntimePodsUnavailable(pods []RuntimePodStatus) bool {
	if len(pods) == 0 {
		return false
	}
	for _, pod := range pods {
		if pod.Ready {
			return false
		}
	}
	return true
}

func hasNonReadyRuntimePod(pods []RuntimePodStatus) bool {
	for _, pod := range pods {
		if !pod.Ready {
			return true
		}
	}
	return false
}

func hasHighRestartCount(pods []RuntimePodStatus) bool {
	for _, pod := range pods {
		if pod.RestartCount >= degradedRestartCount {
			return true
		}
	}
	return false
}
