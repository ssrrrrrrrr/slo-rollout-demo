package main

import (
	"context"
	"log"
	"net/http"
	"time"
)

type IncidentService struct {
	serviceService *ServiceService
	sloService     *SLOService
	runtimeService *RuntimeService
	detector       IncidentDetector
	lifecycle      *IncidentLifecycleService
	readReconcile  bool
}
type currentIncidentObservation struct {
	Candidate      *ReliabilityIncident
	ProvidersKnown bool
	SLO            ServiceSLOStatus
	Runtime        RuntimeSnapshot
	LatestRelease  *ServiceReleaseSummary
}

func NewIncidentService(serviceService *ServiceService, sloService *SLOService, runtimeService *RuntimeService, detector IncidentDetector) *IncidentService {
	return &IncidentService{
		serviceService: serviceService,
		sloService:     sloService,
		runtimeService: runtimeService,
		detector:       detector,
		readReconcile:  true,
	}
}

func (api *portalAPI) incidentService() *IncidentService {
	if api.incidentSvc != nil {
		return api.incidentSvc
	}
	svc := NewIncidentService(api.serviceService(), api.sloService(), api.runtimeService(), NewReliabilityIncidentDetectorWithFreshnessWindow(incidentReleaseFreshnessWindow(api.cfg.IncidentReleaseFreshnessWindow)))
	svc.readReconcile = !api.cfg.ReliabilityControllerEnabled
	repo, err := NewSQLiteIncidentRepository(api.cfg.IncidentStoreDB)
	if err != nil {
		log.Printf("incident persistence unavailable; using observation-only behavior: %v", err)
	} else {
		svc.lifecycle = NewIncidentLifecycleService(svc, repo)
		api.operationService().incidentLifecycle = svc.lifecycle
	}
	api.incidentSvc = svc
	return svc
}

func (svc *IncidentService) ActiveForService(ctx context.Context, r *http.Request, serviceName string) (*ReliabilityIncident, error) {
	if svc.lifecycle != nil {
		if !svc.readReconcile {
			if _, err := svc.serviceService.find(serviceName); err != nil {
				return nil, err
			}
			items, err := svc.lifecycle.repository.FindActiveByService(ctx, serviceName)
			if err != nil {
				return nil, err
			}
			return firstIncident(items), nil
		}
		return svc.lifecycle.ReconcileService(ctx, r, serviceName)
	}
	observation, err := svc.observe(ctx, r, serviceName)
	return observation.Candidate, err
}

// observe is the detector-facing half of incident handling. It deliberately
// has no persistence or state-transition responsibility.
func (svc *IncidentService) observe(ctx context.Context, r *http.Request, serviceName string) (currentIncidentObservation, error) {
	service, err := svc.serviceService.find(serviceName)
	if err != nil {
		return currentIncidentObservation{}, err
	}

	slo, err := svc.sloService.Evaluate(ctx, serviceName)
	providersKnown := err == nil && slo.Status != SLOStatusUnknown
	if err != nil {
		slo = ServiceSLOStatus{Service: serviceName, Status: SLOStatusUnknown, Reason: "SLO provider is unavailable", EvaluatedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	runtime, err := svc.runtimeService.Snapshot(ctx, serviceName)
	providersKnown = providersKnown && err == nil && runtime.Status != RuntimeStatusUnknown
	if err != nil {
		runtime = RuntimeSnapshot{Service: serviceName, Status: RuntimeStatusUnknown, Reason: "Runtime provider is unavailable", ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	}

	var latestRelease *ServiceReleaseSummary
	if svc.serviceService.repository != nil {
		// Evidence is optional correlation context. Its unavailability must not
		// hide an independently detected Service reliability incident.
		latestRelease, _ = svc.serviceService.latestRelease(r, serviceName)
	}

	candidate := svc.Detect(IncidentDetectionInput{
		Service:       service,
		SLO:           slo,
		Runtime:       runtime,
		LatestRelease: latestRelease,
	})
	return currentIncidentObservation{Candidate: candidate, ProvidersKnown: providersKnown, SLO: slo, Runtime: runtime, LatestRelease: latestRelease}, nil
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
	return svc.ListWithQuery(ctx, r, IncidentListQuery{})
}
func (svc *IncidentService) ListWithQuery(ctx context.Context, r *http.Request, query IncidentListQuery) ([]ReliabilityIncident, error) {
	services, err := svc.serviceService.Load()
	if err != nil {
		return nil, err
	}
	if svc.lifecycle != nil {
		if !svc.readReconcile {
			return svc.lifecycle.repository.List(ctx, query)
		}
		if query.Service != "" {
			_, err := svc.lifecycle.ReconcileService(ctx, r, query.Service)
			if err != nil {
				return nil, err
			}
		} else {
			for _, service := range services {
				if _, err := svc.lifecycle.ReconcileService(ctx, r, service.Metadata.Name); err != nil {
					return nil, err
				}
			}
		}
		return svc.lifecycle.repository.List(ctx, query)
	}
	if query.Service != "" {
		incident, err := svc.ActiveForService(ctx, r, query.Service)
		if err != nil {
			return nil, err
		}
		if incident == nil {
			return []ReliabilityIncident{}, nil
		}
		return []ReliabilityIncident{*incident}, nil
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
	if svc.lifecycle != nil {
		incident, err := svc.lifecycle.repository.Get(ctx, id)
		if err != nil {
			if missing, ok := err.(*IncidentNotFoundError); ok {
				missing.ID = id
			}
			return nil, err
		}
		return svc.lifecycle.withTimeline(ctx, incident)
	}
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

func (svc *IncidentService) Timeline(ctx context.Context, id string) ([]IncidentTimelineEvent, error) {
	if svc.lifecycle == nil {
		return nil, &IncidentNotFoundError{ID: id}
	}
	if _, err := svc.lifecycle.repository.Get(ctx, id); err != nil {
		return nil, err
	}
	return svc.lifecycle.repository.ListEvents(ctx, id)
}
func (svc *IncidentService) RecordRecoveryApproval(ctx context.Context, id string, plan RecoveryPlan) {
	if svc.lifecycle != nil {
		svc.lifecycle.RecoveryApproved(ctx, id, plan)
	}
}
func (svc *IncidentService) RecordRecoveryVerification(ctx context.Context, id string, verification RecoveryVerification) {
	if svc.lifecycle != nil {
		svc.lifecycle.RecoveryVerification(ctx, id, verification)
	}
}
func (svc *IncidentService) RecordRemediationVerification(ctx context.Context, id string, verification RemediationVerification) {
	if svc.lifecycle != nil {
		svc.lifecycle.RemediationVerification(ctx, id, verification)
	}
}
func (svc *IncidentService) RecordAgentAnalysis(ctx context.Context, id string, diagnosis AgentDiagnosis) {
	if svc.lifecycle != nil {
		svc.lifecycle.AgentAnalysisCompleted(ctx, id, diagnosis)
	}
}

type IncidentNotFoundError struct {
	ID string
}

func (err *IncidentNotFoundError) Error() string {
	return "incident not found: " + err.ID
}
