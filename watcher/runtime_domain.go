package main

import "time"

type RuntimeStatus string

const (
	RuntimeStatusHealthy   RuntimeStatus = "HEALTHY"
	RuntimeStatusDegraded  RuntimeStatus = "DEGRADED"
	RuntimeStatusUnhealthy RuntimeStatus = "UNHEALTHY"
	RuntimeStatusUnknown   RuntimeStatus = "UNKNOWN"
)

// RuntimeSnapshot is a read-only point-in-time view of a Service workload.
// It contains only workload and pod health, not Kubernetes dashboard details.
type RuntimeSnapshot struct {
	Service      string                `json:"service"`
	Status       RuntimeStatus         `json:"status"`
	Namespace    string                `json:"namespace,omitempty"`
	Workload     RuntimeWorkloadStatus `json:"workload"`
	PrimaryImage string                `json:"primaryImage,omitempty"`
	Images       []string              `json:"images,omitempty"`
	Pods         []RuntimePodStatus    `json:"pods"`
	ObservedAt   string                `json:"observedAt"`
	Reason       string                `json:"reason,omitempty"`
}

type RuntimeWorkloadStatus struct {
	Kind              string `json:"kind,omitempty"`
	Name              string `json:"name,omitempty"`
	Phase             string `json:"phase,omitempty"`
	Revision          string `json:"revision,omitempty"`
	DesiredReplicas   int64  `json:"desiredReplicas"`
	ReadyReplicas     int64  `json:"readyReplicas"`
	AvailableReplicas int64  `json:"availableReplicas"`
	UpdatedReplicas   int64  `json:"updatedReplicas"`
}

type RuntimePodStatus struct {
	Name         string                   `json:"name"`
	Phase        string                   `json:"phase"`
	Ready        bool                     `json:"ready"`
	RestartCount int64                    `json:"restartCount"`
	Node         string                   `json:"node,omitempty"`
	Image        string                   `json:"image,omitempty"`
	CreatedAt    string                   `json:"createdAt,omitempty"`
	Containers   []RuntimeContainerStatus `json:"containers,omitempty"`
}

type RuntimeContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int64  `json:"restartCount"`
	Image        string `json:"image,omitempty"`
}

func newUnknownRuntimeSnapshot(service Service, reason string) RuntimeSnapshot {
	return RuntimeSnapshot{
		Service:   service.Metadata.Name,
		Status:    RuntimeStatusUnknown,
		Namespace: service.Runtime.Namespace,
		Workload: RuntimeWorkloadStatus{
			Kind: service.Runtime.Workload.Kind,
			Name: service.Runtime.Workload.Name,
		},
		Pods:       []RuntimePodStatus{},
		ObservedAt: time.Now().Format(time.RFC3339),
		Reason:     reason,
	}
}
