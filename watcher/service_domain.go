package main

// Service is the configuration-backed domain object that connects a workload,
// its reliability contract, delivery strategy, and release history.
type Service struct {
	APIVersion   string                `yaml:"apiVersion" json:"apiVersion"`
	Kind         string                `yaml:"kind" json:"kind"`
	Metadata     ServiceMetadata       `yaml:"metadata" json:"metadata"`
	Spec         ServiceSpec           `yaml:"spec" json:"spec"`
	Environments []string              `yaml:"environments" json:"environments"`
	Runtime      ServiceRuntimeRef     `yaml:"runtime" json:"runtime"`
	Reliability  ServiceReliabilityRef `yaml:"reliability" json:"reliability"`
	Delivery     ServiceDeliveryRef    `yaml:"delivery" json:"delivery"`
}

type ServiceMetadata struct {
	Name string `yaml:"name" json:"name"`
}

type ServiceSpec struct {
	DisplayName string `yaml:"displayName" json:"displayName"`
	Owner       string `yaml:"owner" json:"owner"`
}

type ServiceRuntimeRef struct {
	Namespace string             `yaml:"namespace" json:"namespace"`
	Workload  ServiceWorkloadRef `yaml:"workload" json:"workload"`
}

type ServiceWorkloadRef struct {
	Kind string `yaml:"kind" json:"kind"`
	Name string `yaml:"name" json:"name"`
}

// ServiceReliabilityRef intentionally contains only today's SLO reference;
// future SLO and incident capabilities can extend this boundary independently.
type ServiceReliabilityRef struct {
	SLORef string `yaml:"sloRef" json:"sloRef"`
}

// ServiceDeliveryRef intentionally contains only today's strategy reference.
type ServiceDeliveryRef struct {
	StrategyRef string `yaml:"strategyRef" json:"strategyRef"`
}

type ServiceReleaseSummary struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type ServiceSummary struct {
	Name          string                 `json:"name"`
	DisplayName   string                 `json:"displayName"`
	Owner         string                 `json:"owner"`
	Environments  []string               `json:"environments"`
	Runtime       ServiceRuntimeRef      `json:"runtime"`
	SLORef        string                 `json:"sloRef"`
	StrategyRef   string                 `json:"strategyRef"`
	LatestRelease *ServiceReleaseSummary `json:"latestRelease"`
}

func (service Service) Summary(latestRelease *ServiceReleaseSummary) ServiceSummary {
	return ServiceSummary{
		Name:          service.Metadata.Name,
		DisplayName:   service.Spec.DisplayName,
		Owner:         service.Spec.Owner,
		Environments:  service.Environments,
		Runtime:       service.Runtime,
		SLORef:        service.Reliability.SLORef,
		StrategyRef:   service.Delivery.StrategyRef,
		LatestRelease: latestRelease,
	}
}
