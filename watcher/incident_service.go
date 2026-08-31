package main

import (
	"context"
	"net/http"
)

type IncidentService struct {
	serviceService *ServiceService
	sloService     *SLOService
	runtimeService *RuntimeService
	detector       IncidentDetector
}

func NewIncidentService(serviceService *ServiceService, sloService *SLOService, runtimeService *RuntimeService, detector IncidentDetector) *IncidentService {
	return &IncidentService{
		serviceService: serviceService,
		sloService:     sloService,
		runtimeService: runtimeService,
		detector:       detector,
	}
}

func (api *portalAPI) incidentService() *IncidentService {
	if api.incidentSvc != nil {
		return api.incidentSvc
	}
	return NewIncidentService(api.serviceService(), api.sloService(), api.runtimeService(), NewReliabilityIncidentDetectorWithFreshnessWindow(incidentReleaseFreshnessWindow(api.cfg.IncidentReleaseFreshnessWindow)))
}

func (svc *IncidentService) ActiveForService(ctx context.Context, r *http.Request, serviceName string) (*ReliabilityIncident, error) {
	service, err := svc.serviceService.find(serviceName)
	if err != nil {
		return nil, err
	}

	slo, err := svc.sloService.Evaluate(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	runtime, err := svc.runtimeService.Snapshot(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	var latestRelease *ServiceReleaseSummary
	if svc.serviceService.repository != nil {
		// Evidence is optional correlation context. Its unavailability must not
		// hide an independently detected Service reliability incident.
		latestRelease, _ = svc.serviceService.latestRelease(r, serviceName)
	}

	return svc.Detect(IncidentDetectionInput{
		Service:       service,
		SLO:           slo,
		Runtime:       runtime,
		LatestRelease: latestRelease,
	}), nil
}

// Detect reuses the existing IncidentDetector when a caller already has the
// current Service, SLO, Runtime, and Release inputs available.
func (svc *IncidentService) Detect(input IncidentDetectionInput) *ReliabilityIncident {
	return svc.detector.Detect(input)
}

func (svc *IncidentService) IsCurrentReleaseFailure(release *ServiceReleaseSummary) bool {
	return svc.detector.IsCurrentReleaseFailure(release)
}

func (svc *IncidentService) IsCurrentRelease(release *ServiceReleaseSummary) bool {
	return svc.detector.IsCurrentRelease(release)
}

func (svc *IncidentService) List(ctx context.Context, r *http.Request) ([]ReliabilityIncident, error) {
	services, err := svc.serviceService.Load()
	if err != nil {
		return nil, err
	}

	incidents := make([]ReliabilityIncident, 0, len(services))
	for _, service := range services {
		incident, err := svc.ActiveForService(ctx, r, service.Metadata.Name)
		if err != nil {
			return nil, err
		}
		if incident != nil {
			incidents = append(incidents, *incident)
		}
	}
	return incidents, nil
}

func (svc *IncidentService) Get(ctx context.Context, r *http.Request, id string) (*ReliabilityIncident, error) {
	incidents, err := svc.List(ctx, r)
	if err != nil {
		return nil, err
	}
	for index := range incidents {
		if incidents[index].ID == id {
			return &incidents[index], nil
		}
	}
	return nil, &IncidentNotFoundError{ID: id}
}

type IncidentNotFoundError struct {
	ID string
}

func (err *IncidentNotFoundError) Error() string {
	return "incident not found: " + err.ID
}
