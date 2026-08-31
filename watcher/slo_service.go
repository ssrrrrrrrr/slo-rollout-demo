package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type SLOProvider interface {
	Evaluate(ctx context.Context, service Service, config ServiceSLOConfig) (ServiceSLOStatus, error)
}

type SLOService struct {
	serviceService *ServiceService
	configDir      string
	provider       SLOProvider
}

func NewSLOService(repoDir string, serviceService *ServiceService, provider SLOProvider) *SLOService {
	return &SLOService{
		serviceService: serviceService,
		configDir:      filepath.Join(repoDir, "configs", "services"),
		provider:       provider,
	}
}

func (api *portalAPI) sloService() *SLOService {
	if api.sloSvc != nil {
		return api.sloSvc
	}

	return NewSLOService(api.cfg.RepoDir, api.serviceService(), NewPrometheusSLOProvider(api.cfg.PrometheusURL))
}

func (svc *SLOService) Evaluate(ctx context.Context, serviceName string) (ServiceSLOStatus, error) {
	service, err := svc.serviceService.find(serviceName)
	if err != nil {
		return ServiceSLOStatus{}, err
	}

	sloRef := strings.TrimSpace(service.Reliability.SLORef)
	if sloRef == "" {
		return newUnknownServiceSLOStatus(service.Metadata.Name, "SLO is not configured for this service"), nil
	}

	config, err := svc.ResolveConfig(sloRef)
	if err != nil {
		return newUnknownServiceSLOStatus(service.Metadata.Name, fmt.Sprintf("SLO config %q is unavailable: %v", sloRef, err)), nil
	}

	status, err := svc.provider.Evaluate(ctx, service, config)
	if err != nil {
		return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("SLO provider unavailable: %v", err)), nil
	}

	return status, nil
}

func (svc *SLOService) ResolveConfig(name string) (ServiceSLOConfig, error) {
	files, err := filepath.Glob(filepath.Join(svc.configDir, "*.slo.yaml"))
	if err != nil {
		return ServiceSLOConfig{}, fmt.Errorf("find SLO configs: %w", err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return ServiceSLOConfig{}, fmt.Errorf("read SLO config %s: %w", file, err)
		}

		var config ServiceSLOConfig
		if err := yaml.Unmarshal(data, &config); err != nil {
			return ServiceSLOConfig{}, fmt.Errorf("parse SLO config %s: %w", file, err)
		}
		if config.Metadata.Name == name {
			return config, nil
		}
	}

	return ServiceSLOConfig{}, fmt.Errorf("not found")
}

type ServiceSLOConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   SLOConfigMetadata `yaml:"metadata"`
	Spec       SLOConfigSpec     `yaml:"spec"`
}

type SLOConfigMetadata struct {
	Name      string `yaml:"name"`
	Service   string `yaml:"service"`
	Namespace string `yaml:"namespace"`
	Env       string `yaml:"env"`
}

type SLOConfigSpec struct {
	ServiceLevel  ServiceLevelSLOConfig `yaml:"serviceLevel"`
	Observability SLOObservability      `yaml:"observability"`
	Objectives    []SLOConfigObjective  `yaml:"objectives"`
}

type ServiceLevelSLOConfig struct {
	Window             string  `yaml:"window"`
	AvailabilityTarget float64 `yaml:"availabilityTarget"`
}

type SLOObservability struct {
	Prometheus SLOPrometheusConfig `yaml:"prometheus"`
}

type SLOPrometheusConfig struct {
	RequestCounter   string              `yaml:"requestCounter"`
	LatencyHistogram string              `yaml:"latencyHistogram"`
	ErrorStatusRegex string              `yaml:"errorStatusRegex"`
	Labels           SLOPrometheusLabels `yaml:"labels"`
}

type SLOPrometheusLabels struct {
	Namespace string `yaml:"namespace"`
	Status    string `yaml:"status"`
}

type SLOConfigObjective struct {
	ID         string             `yaml:"id"`
	Name       string             `yaml:"name"`
	Type       string             `yaml:"type"`
	Percentile int                `yaml:"percentile"`
	Threshold  SLOConfigThreshold `yaml:"threshold"`
}

type SLOConfigThreshold struct {
	Value float64 `yaml:"value"`
	Unit  string  `yaml:"unit"`
}

func (config ServiceSLOConfig) latencyTargetMilliseconds() (float64, bool) {
	for _, objective := range config.Spec.Objectives {
		if objective.Type != "latency" {
			continue
		}

		switch objective.Threshold.Unit {
		case "seconds":
			return objective.Threshold.Value * 1000, true
		case "milliseconds", "ms":
			return objective.Threshold.Value, true
		}
	}

	return 0, false
}
