package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServiceService struct {
	configDir  string
	repository EvidenceRepository
}

func NewServiceService(repoDir string, repository EvidenceRepository) *ServiceService {
	return &ServiceService{
		configDir:  filepath.Join(repoDir, "configs", "services"),
		repository: repository,
	}
}

func (api *portalAPI) serviceService() *ServiceService {
	if api.serviceSvc != nil {
		return api.serviceSvc
	}

	return NewServiceService(api.cfg.RepoDir, api.evidenceRepository())
}

func (svc *ServiceService) Load() ([]Service, error) {
	files, err := filepath.Glob(filepath.Join(svc.configDir, "*.service.yaml"))
	if err != nil {
		return nil, fmt.Errorf("find service configs: %w", err)
	}

	sort.Strings(files)
	services := make([]Service, 0, len(files))
	seenNames := map[string]struct{}{}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read service config %s: %w", file, err)
		}

		var service Service
		if err := yaml.Unmarshal(data, &service); err != nil {
			return nil, fmt.Errorf("parse service config %s: %w", file, err)
		}
		if err := validateService(service); err != nil {
			return nil, fmt.Errorf("invalid service config %s: %w", file, err)
		}
		if _, exists := seenNames[service.Metadata.Name]; exists {
			return nil, fmt.Errorf("duplicate service name %q", service.Metadata.Name)
		}

		seenNames[service.Metadata.Name] = struct{}{}
		services = append(services, service)
	}

	return services, nil
}

func (svc *ServiceService) List(r *http.Request) ([]ServiceSummary, error) {
	services, err := svc.Load()
	if err != nil {
		return nil, err
	}

	summaries := make([]ServiceSummary, 0, len(services))
	for _, service := range services {
		latestRelease, err := svc.latestRelease(r, service.Metadata.Name)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, service.Summary(latestRelease))
	}

	return summaries, nil
}

func (svc *ServiceService) Get(r *http.Request, name string) (ServiceSummary, error) {
	service, err := svc.find(name)
	if err != nil {
		return ServiceSummary{}, err
	}

	latestRelease, err := svc.latestRelease(r, service.Metadata.Name)
	if err != nil {
		return ServiceSummary{}, err
	}

	return service.Summary(latestRelease), nil
}

func (svc *ServiceService) Releases(r *http.Request, name string) ([]ServiceReleaseSummary, error) {
	if _, err := svc.find(name); err != nil {
		return nil, err
	}

	return svc.releases(r, name, "50")
}

func (svc *ServiceService) find(name string) (Service, error) {
	services, err := svc.Load()
	if err != nil {
		return Service{}, err
	}

	for _, service := range services {
		if service.Metadata.Name == name {
			return service, nil
		}
	}

	return Service{}, &ServiceNotFoundError{Name: name}
}

func (svc *ServiceService) latestRelease(r *http.Request, serviceName string) (*ServiceReleaseSummary, error) {
	releases, err := svc.releases(r, serviceName, "1")
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, nil
	}

	return &releases[0], nil
}

func (svc *ServiceService) releases(r *http.Request, serviceName, limit string) ([]ServiceReleaseSummary, error) {
	response, err := svc.repository.ListReleases(r, EvidenceReleaseListQuery{
		Limit:   limit,
		Service: serviceName,
	})
	if err != nil {
		return nil, err
	}

	var body struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(response.Body, &body); err != nil {
		return nil, fmt.Errorf("decode evidence release list: %w", err)
	}

	releases := make([]ServiceReleaseSummary, 0, len(body.Items))
	for _, item := range body.Items {
		id := serviceReleaseString(item, "release_id")
		if id == "" {
			continue
		}

		releases = append(releases, ServiceReleaseSummary{
			ID:        id,
			Status:    firstServiceReleaseValue(item, "release_result", "final_action", "policy_decision"),
			Timestamp: firstServiceReleaseValue(item, "generated_at", "last_seen_at", "first_seen_at"),
		})
	}

	return releases, nil
}

func validateService(service Service) error {
	if service.APIVersion != "sentinel.io/v1alpha1" {
		return fmt.Errorf("apiVersion must be sentinel.io/v1alpha1")
	}
	if service.Kind != "Service" {
		return fmt.Errorf("kind must be Service")
	}
	if strings.TrimSpace(service.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if strings.TrimSpace(service.Spec.DisplayName) == "" || strings.TrimSpace(service.Spec.Owner) == "" {
		return fmt.Errorf("spec.displayName and spec.owner are required")
	}
	if len(service.Environments) == 0 || strings.TrimSpace(service.Runtime.Namespace) == "" || strings.TrimSpace(service.Runtime.Workload.Kind) == "" || strings.TrimSpace(service.Runtime.Workload.Name) == "" {
		return fmt.Errorf("environments and runtime reference are required")
	}
	if strings.TrimSpace(service.Reliability.SLORef) == "" || strings.TrimSpace(service.Delivery.StrategyRef) == "" {
		return fmt.Errorf("reliability.sloRef and delivery.strategyRef are required")
	}

	return nil
}

func serviceReleaseString(item map[string]interface{}, key string) string {
	value, ok := item[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstServiceReleaseValue(item map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := serviceReleaseString(item, key); value != "" {
			return value
		}
	}
	return ""
}

type ServiceNotFoundError struct {
	Name string
}

func (err *ServiceNotFoundError) Error() string {
	return fmt.Sprintf("service not found: %s", err.Name)
}
