package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	maxOverviewServiceConcurrency = 4
	lowErrorBudgetRemaining       = 20.0
)

type OverviewService struct {
	serviceService  *ServiceService
	sloService      *SLOService
	runtimeService  *RuntimeService
	incidentService *IncidentService
}

func NewOverviewService(serviceService *ServiceService, sloService *SLOService, runtimeService *RuntimeService, incidentService *IncidentService) *OverviewService {
	return &OverviewService{serviceService: serviceService, sloService: sloService, runtimeService: runtimeService, incidentService: incidentService}
}

func (api *portalAPI) overviewService() *OverviewService {
	if api.overviewSvc != nil {
		return api.overviewSvc
	}
	return NewOverviewService(api.serviceService(), api.sloService(), api.runtimeService(), api.incidentService())
}

func (svc *OverviewService) Get(ctx context.Context, r *http.Request) (ReliabilityOverview, error) {
	services, err := svc.serviceService.Load()
	if err != nil {
		return ReliabilityOverview{}, err
	}

	summaries := make([]ServiceReliabilitySummary, len(services))
	incidents := make([]*ReliabilityIncident, len(services))
	semaphore := make(chan struct{}, maxOverviewServiceConcurrency)
	var waitGroup sync.WaitGroup
	for index := range services {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				summaries[index] = unknownOverviewSummary(services[index], ctx.Err())
				return
			}
			defer func() { <-semaphore }()
			summaries[index], incidents[index] = svc.evaluateService(ctx, r, services[index])
		}(index)
	}
	waitGroup.Wait()

	overview := ReliabilityOverview{
		SchemaVersion: "reliability.overview/v1alpha1",
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Services:      summaries,
	}
	overview.Summary = summarizeFleet(summaries, incidents, svc.incidentService.IsCurrentReleaseFailure)
	overview.Attention = buildAttention(summaries, incidents)
	return overview, nil
}

func (svc *OverviewService) evaluateService(ctx context.Context, r *http.Request, service Service) (ServiceReliabilitySummary, *ReliabilityIncident) {
	var slo ServiceSLOStatus
	var runtime RuntimeSnapshot
	var sloErr, runtimeErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		slo, sloErr = svc.sloService.Evaluate(ctx, service.Metadata.Name)
	}()
	go func() {
		defer waitGroup.Done()
		runtime, runtimeErr = svc.runtimeService.Snapshot(ctx, service.Metadata.Name)
	}()
	waitGroup.Wait()

	if sloErr != nil {
		slo = newUnknownServiceSLOStatus(service.Metadata.Name, fmt.Sprintf("SLO evaluation unavailable: %v", sloErr))
	}
	if runtimeErr != nil {
		runtime = newUnknownRuntimeSnapshot(service, fmt.Sprintf("Runtime evaluation unavailable: %v", runtimeErr))
	}

	var latestRelease *ServiceReleaseSummary
	if svc.serviceService.repository != nil {
		latestRelease, _ = svc.serviceService.latestRelease(r, service.Metadata.Name)
	}
	incident := svc.incidentService.Detect(IncidentDetectionInput{Service: service, SLO: slo, Runtime: runtime, LatestRelease: latestRelease})
	summary := ServiceReliabilitySummary{
		Name:                 service.Metadata.Name,
		DisplayName:          service.Spec.DisplayName,
		SLOStatus:            slo.Status,
		RuntimeStatus:        runtime.Status,
		LatestRelease:        latestRelease,
		ErrorBudgetRemaining: slo.ErrorBudget.RemainingPercent,
		BurnRate1h:           slo.BurnRate.OneHour,
		ObservedAt:           time.Now().Format(time.RFC3339),
	}
	if incident != nil {
		summary.IncidentStatus = incident.Status
		summary.IncidentSeverity = incident.Severity
		summary.IncidentID = incident.ID
	}
	summary.OverallStatus = overallServiceStatus(summary)
	return summary, incident
}

func unknownOverviewSummary(service Service, reason error) ServiceReliabilitySummary {
	_ = reason
	return ServiceReliabilitySummary{Name: service.Metadata.Name, DisplayName: service.Spec.DisplayName, OverallStatus: ReliabilityOverallUnknown, SLOStatus: SLOStatusUnknown, RuntimeStatus: RuntimeStatusUnknown, ObservedAt: time.Now().Format(time.RFC3339)}
}

func overallServiceStatus(summary ServiceReliabilitySummary) ReliabilityOverallStatus {
	if summary.IncidentStatus == IncidentStatusActive && (summary.IncidentSeverity == IncidentSeveritySEV1 || summary.IncidentSeverity == IncidentSeveritySEV2) || summary.RuntimeStatus == RuntimeStatusUnhealthy || summary.SLOStatus == SLOStatusBreached {
		return ReliabilityOverallUnhealthy
	}
	if summary.IncidentStatus == IncidentStatusActive && summary.IncidentSeverity == IncidentSeveritySEV3 || summary.SLOStatus == SLOStatusAtRisk || summary.RuntimeStatus == RuntimeStatusDegraded || (summary.ErrorBudgetRemaining != nil && *summary.ErrorBudgetRemaining < lowErrorBudgetRemaining) || (summary.BurnRate1h != nil && *summary.BurnRate1h >= 1) {
		return ReliabilityOverallAtRisk
	}
	if summary.SLOStatus == SLOStatusHealthy && summary.RuntimeStatus == RuntimeStatusHealthy && summary.IncidentStatus == "" {
		return ReliabilityOverallHealthy
	}
	return ReliabilityOverallUnknown
}

func summarizeFleet(services []ServiceReliabilitySummary, incidents []*ReliabilityIncident, isCurrentReleaseFailure func(*ServiceReleaseSummary) bool) FleetSummary {
	summary := FleetSummary{TotalServices: len(services)}
	for index, service := range services {
		switch service.OverallStatus {
		case ReliabilityOverallHealthy:
			summary.HealthyServices++
		case ReliabilityOverallAtRisk:
			summary.AtRiskServices++
		case ReliabilityOverallUnhealthy:
			summary.UnhealthyServices++
		default:
			summary.UnknownServices++
		}
		if service.SLOStatus == SLOStatusBreached {
			summary.SLOBreaches++
		}
		if service.RuntimeStatus == RuntimeStatusUnhealthy {
			summary.RuntimeUnhealthy++
		}
		if service.RuntimeStatus == RuntimeStatusDegraded {
			summary.RuntimeDegraded++
		}
		if isCurrentReleaseFailure(service.LatestRelease) {
			summary.ReleaseRisks++
		}
		if incident := incidents[index]; incident != nil {
			summary.ActiveIncidents++
			switch incident.Severity {
			case IncidentSeveritySEV1:
				summary.SEV1Incidents++
			case IncidentSeveritySEV2:
				summary.SEV2Incidents++
			case IncidentSeveritySEV3:
				summary.SEV3Incidents++
			}
		}
	}
	return summary
}

func buildAttention(services []ServiceReliabilitySummary, incidents []*ReliabilityIncident) []AttentionItem {
	items := make([]AttentionItem, 0, len(services))
	for index, service := range services {
		if item, ok := attentionForService(service, incidents[index]); ok {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(left, right int) bool {
		leftRank, rightRank := attentionPriorityRank(items[left].Priority), attentionPriorityRank(items[right].Priority)
		if leftRank == rightRank {
			return items[left].Service < items[right].Service
		}
		return leftRank < rightRank
	})
	return items
}

func attentionForService(service ServiceReliabilitySummary, incident *ReliabilityIncident) (AttentionItem, bool) {
	relatedRelease := incidentRelatedRelease(service.LatestRelease)
	if incident != nil {
		priority := "MEDIUM"
		if incident.Severity == IncidentSeveritySEV1 {
			priority = "CRITICAL"
		} else if incident.Severity == IncidentSeveritySEV2 {
			priority = "HIGH"
		}
		return AttentionItem{Service: service.Name, Priority: priority, Type: "INCIDENT", Title: "Active " + string(incident.Severity) + " reliability incident", Reason: incident.Title, RelatedIncident: incident.ID, RelatedRelease: relatedRelease, ActionTarget: "INCIDENT"}, true
	}
	if service.SLOStatus == SLOStatusBreached {
		return AttentionItem{Service: service.Name, Priority: "HIGH", Type: "SLO_BREACH", Title: "SLO breached", Reason: "Service reliability target is breached", RelatedRelease: relatedRelease, ActionTarget: "SERVICE"}, true
	}
	if service.RuntimeStatus == RuntimeStatusUnhealthy {
		return AttentionItem{Service: service.Name, Priority: "HIGH", Type: "RUNTIME_UNHEALTHY", Title: "Runtime unhealthy", Reason: "No healthy runtime state is available", RelatedRelease: relatedRelease, ActionTarget: "SERVICE"}, true
	}
	if service.SLOStatus == SLOStatusAtRisk {
		return AttentionItem{Service: service.Name, Priority: "MEDIUM", Type: "SLO_AT_RISK", Title: "SLO at risk", Reason: "Service reliability is approaching its objective", RelatedRelease: relatedRelease, ActionTarget: "SERVICE"}, true
	}
	if service.RuntimeStatus == RuntimeStatusDegraded {
		return AttentionItem{Service: service.Name, Priority: "MEDIUM", Type: "RUNTIME_DEGRADED", Title: "Runtime degraded", Reason: "Runtime replicas or pods are below healthy state", RelatedRelease: relatedRelease, ActionTarget: "SERVICE"}, true
	}
	if service.BurnRate1h != nil && *service.BurnRate1h >= 1 {
		return AttentionItem{Service: service.Name, Priority: "MEDIUM", Type: "BURN_RATE", Title: "Burn rate is elevated", Reason: "One-hour burn rate is at or above 1x", RelatedRelease: relatedRelease, ActionTarget: "SERVICE"}, true
	}
	if service.ErrorBudgetRemaining != nil && *service.ErrorBudgetRemaining < lowErrorBudgetRemaining {
		return AttentionItem{Service: service.Name, Priority: "MEDIUM", Type: "ERROR_BUDGET", Title: "Error budget is low", Reason: "Remaining error budget is below 20%", RelatedRelease: relatedRelease, ActionTarget: "SERVICE"}, true
	}
	return AttentionItem{}, false
}

func attentionPriorityRank(priority string) int {
	switch priority {
	case "CRITICAL":
		return 1
	case "HIGH":
		return 2
	case "MEDIUM":
		return 3
	default:
		return 4
	}
}
