package main

import (
	"context"
	"fmt"
	"strings"
)

type RuntimeProvider interface {
	Snapshot(ctx context.Context, service Service) (RuntimeSnapshot, error)
}

type RuntimeService struct {
	serviceService *ServiceService
	provider       RuntimeProvider
}

func NewRuntimeService(serviceService *ServiceService, provider RuntimeProvider) *RuntimeService {
	return &RuntimeService{
		serviceService: serviceService,
		provider:       provider,
	}
}

func (api *portalAPI) runtimeService() *RuntimeService {
	if api.runtimeSvc != nil {
		return api.runtimeSvc
	}

	return NewRuntimeService(api.serviceService(), NewKubernetesRuntimeProvider())
}

func (svc *RuntimeService) Snapshot(ctx context.Context, serviceName string) (RuntimeSnapshot, error) {
	service, err := svc.serviceService.find(serviceName)
	if err != nil {
		return RuntimeSnapshot{}, err
	}

	if strings.TrimSpace(service.Runtime.Namespace) == "" || strings.TrimSpace(service.Runtime.Workload.Kind) == "" || strings.TrimSpace(service.Runtime.Workload.Name) == "" {
		return newUnknownRuntimeSnapshot(service, "runtime is not configured for this service"), nil
	}

	snapshot, err := svc.provider.Snapshot(ctx, service)
	if err != nil {
		return newUnknownRuntimeSnapshot(service, fmt.Sprintf("Kubernetes runtime data unavailable: %v", err)), nil
	}

	return snapshot, nil
}
