package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type IncidentDetectionInput struct {
	Service       Service
	SLO           ServiceSLOStatus
	Runtime       RuntimeSnapshot
	LatestRelease *ServiceReleaseSummary
}

type IncidentDetector interface {
	Detect(input IncidentDetectionInput) *ReliabilityIncident
	IsCurrentReleaseFailure(release *ServiceReleaseSummary) bool
}

// DefaultIncidentReleaseFreshnessWindow keeps Release-only incidents bounded to
// failures that still have current runtime relevance.
const DefaultIncidentReleaseFreshnessWindow = time.Hour
const DefaultIncidentReleaseFreshnessWindowText = "1h"

type ReliabilityIncidentDetector struct {
	releaseFreshnessWindow time.Duration
	now                    func() time.Time
}

func NewReliabilityIncidentDetector() *ReliabilityIncidentDetector {
	return NewReliabilityIncidentDetectorWithFreshnessWindow(DefaultIncidentReleaseFreshnessWindow)
}

func NewReliabilityIncidentDetectorWithFreshnessWindow(window time.Duration) *ReliabilityIncidentDetector {
	if window <= 0 {
		window = DefaultIncidentReleaseFreshnessWindow
	}
	return &ReliabilityIncidentDetector{releaseFreshnessWindow: window, now: time.Now}
}

func incidentReleaseFreshnessWindow(value string) time.Duration {
	window, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || window <= 0 {
		return DefaultIncidentReleaseFreshnessWindow
	}
	return window
}

func (detector *ReliabilityIncidentDetector) Detect(input IncidentDetectionInput) *ReliabilityIncident {
	releaseFailureActive := detector.IsCurrentReleaseFailure(input.LatestRelease)
	signals := incidentSignals(input, releaseFailureActive)
	primary := primaryIncidentSignal(signals, releaseFailureActive)
	if primary == nil {
		return nil
	}

	observedAt := incidentObservedAt()
	relatedRelease := incidentRelatedRelease(input.LatestRelease)
	key := input.Service.Metadata.Name + ":" + primary.Type
	if relatedRelease != nil {
		key += ":" + relatedRelease.ID
	}

	incident := &ReliabilityIncident{
		ID:              deterministicIncidentID(key),
		Service:         input.Service.Metadata.Name,
		Status:          IncidentStatusActive,
		Severity:        incidentSeverity(input, releaseFailureActive),
		Title:           incidentTitle(input.Service.Metadata.Name, primary.Type),
		PrimarySignal:   *primary,
		Signals:         signals,
		RelatedRelease:  relatedRelease,
		Recommendation:  incidentRecommendation(input.LatestRelease),
		ReleaseEvidence: incidentReleaseEvidence(input.LatestRelease),
		SLO:             input.SLO,
		Runtime:         input.Runtime,
		StartedAt:       incidentStartedAt(input, observedAt),
		ObservedAt:      observedAt,
	}
	incident.Timeline = synthesizeIncidentTimeline(incident)
	return incident
}

func incidentSignals(input IncidentDetectionInput, releaseFailureActive bool) []IncidentSignal {
	signals := make([]IncidentSignal, 0, 3)
	switch input.SLO.Status {
	case SLOStatusBreached:
		signals = append(signals, IncidentSignal{Type: "SLO_BREACH", Status: string(input.SLO.Status), Reason: input.SLO.Reason})
	case SLOStatusAtRisk:
		signals = append(signals, IncidentSignal{Type: "SLO_AT_RISK", Status: string(input.SLO.Status), Reason: input.SLO.Reason})
	}

	switch input.Runtime.Status {
	case RuntimeStatusUnhealthy:
		signals = append(signals, IncidentSignal{Type: "RUNTIME_UNHEALTHY", Status: string(input.Runtime.Status), Reason: input.Runtime.Reason})
	case RuntimeStatusDegraded:
		signals = append(signals, IncidentSignal{Type: "RUNTIME_DEGRADED", Status: string(input.Runtime.Status), Reason: input.Runtime.Reason})
	}

	if release := input.LatestRelease; release != nil && strings.TrimSpace(release.Status) != "" {
		reason := "latest release evidence is temporally correlated"
		if isReleaseFailure(release.Status) && !releaseFailureActive {
			reason = "latest failed release is historical or has no usable timestamp; temporal correlation only"
		}
		signals = append(signals, IncidentSignal{
			Type:   releaseSignalType(release.Status),
			Status: release.Status,
			Reason: reason,
		})
	}

	return signals
}

func primaryIncidentSignal(signals []IncidentSignal, releaseFailureActive bool) *IncidentSignal {
	for index := range signals {
		if signals[index].Type == "SLO_BREACH" {
			return &signals[index]
		}
	}
	for index := range signals {
		if signals[index].Type == "RUNTIME_UNHEALTHY" {
			return &signals[index]
		}
	}
	for index := range signals {
		if releaseFailureActive && strings.HasPrefix(signals[index].Type, "RELEASE_") && isReleaseFailure(signals[index].Status) {
			return &signals[index]
		}
	}
	return nil
}

func incidentSeverity(input IncidentDetectionInput, releaseFailureActive bool) IncidentSeverity {
	sloBreached := input.SLO.Status == SLOStatusBreached
	runtimeUnhealthy := input.Runtime.Status == RuntimeStatusUnhealthy
	releaseFailed := releaseFailureActive && input.LatestRelease != nil && isReleaseFailure(input.LatestRelease.Status)

	switch {
	case sloBreached && runtimeUnhealthy:
		return IncidentSeveritySEV1
	case sloBreached || (runtimeUnhealthy && releaseFailed):
		return IncidentSeveritySEV2
	case runtimeUnhealthy || releaseFailed:
		return IncidentSeveritySEV3
	default:
		return IncidentSeveritySEV4
	}
}

// IsCurrentReleaseFailure is the single active-relevance rule for failed
// Release evidence. Consumers may retain stale Release data as correlation,
// but only this helper permits it to affect current reliability state.
func (detector *ReliabilityIncidentDetector) IsCurrentReleaseFailure(release *ServiceReleaseSummary) bool {
	now := time.Now
	if detector.now != nil {
		now = detector.now
	}
	return isCurrentReleaseFailure(release, now(), detector.releaseFreshnessWindow)
}

func isCurrentReleaseFailure(release *ServiceReleaseSummary, now time.Time, freshnessWindow time.Duration) bool {
	if release == nil || !isReleaseFailure(release.Status) || strings.TrimSpace(release.Timestamp) == "" {
		return false
	}
	timestamp, err := time.Parse(time.RFC3339, release.Timestamp)
	if err != nil {
		return false
	}
	age := now.Sub(timestamp)
	return age >= 0 && age <= freshnessWindow
}

func deterministicIncidentID(key string) string {
	hash := sha256.Sum256([]byte(key))
	return "INC-" + hex.EncodeToString(hash[:])[:8]
}

func incidentRelatedRelease(release *ServiceReleaseSummary) *IncidentCorrelation {
	if release == nil || strings.TrimSpace(release.ID) == "" {
		return nil
	}
	return &IncidentCorrelation{ID: release.ID, Status: release.Status, Correlation: "TEMPORAL", Timestamp: release.Timestamp}
}

func incidentRecommendation(release *ServiceReleaseSummary) *IncidentRecommendation {
	if release == nil || strings.TrimSpace(release.FinalAction) == "" {
		return nil
	}
	return &IncidentRecommendation{Action: release.FinalAction, Source: "release evidence"}
}

func incidentReleaseEvidence(release *ServiceReleaseSummary) *IncidentReleaseEvidence {
	if release == nil || (strings.TrimSpace(release.PolicyDecision) == "" && strings.TrimSpace(release.FinalAction) == "") {
		return nil
	}
	return &IncidentReleaseEvidence{PolicyDecision: release.PolicyDecision, FinalAction: release.FinalAction}
}

func releaseSignalType(status string) string {
	normalized := strings.ToUpper(strings.TrimSpace(status))
	normalized = strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(normalized)
	if normalized == "" {
		normalized = "UNKNOWN"
	}
	return "RELEASE_" + normalized
}

func isReleaseFailure(status string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(status))
	return strings.Contains(normalized, "FAIL") || strings.Contains(normalized, "ABORT") || strings.Contains(normalized, "DEGRADED") || strings.Contains(normalized, "DENY") || strings.Contains(normalized, "BLOCK") || strings.Contains(normalized, "ERROR")
}

func incidentTitle(service, primaryType string) string {
	switch primaryType {
	case "SLO_BREACH":
		return "SLO breach detected for " + service
	case "RUNTIME_UNHEALTHY":
		return "Runtime unhealthy for " + service
	default:
		return "Release failure detected for " + service
	}
}

func incidentStartedAt(input IncidentDetectionInput, fallback string) string {
	for _, candidate := range []string{input.SLO.EvaluatedAt, input.Runtime.ObservedAt} {
		if _, err := time.Parse(time.RFC3339, candidate); err == nil {
			return candidate
		}
	}
	if input.LatestRelease != nil {
		if _, err := time.Parse(time.RFC3339, input.LatestRelease.Timestamp); err == nil {
			return input.LatestRelease.Timestamp
		}
	}
	return fallback
}

func synthesizeIncidentTimeline(incident *ReliabilityIncident) []IncidentTimelineEvent {
	events := make([]IncidentTimelineEvent, 0, 3)
	for _, signal := range incident.Signals {
		switch signal.Type {
		case "SLO_BREACH":
			events = append(events, IncidentTimelineEvent{Type: signal.Type, Message: "SLO breach detected", OccurredAt: incident.SLO.EvaluatedAt})
		case "RUNTIME_UNHEALTHY", "RUNTIME_DEGRADED":
			events = append(events, IncidentTimelineEvent{Type: signal.Type, Message: "Runtime " + strings.ToLower(signal.Status) + " observed", OccurredAt: incident.Runtime.ObservedAt})
		default:
			if strings.HasPrefix(signal.Type, "RELEASE_") && incident.RelatedRelease != nil {
				events = append(events, IncidentTimelineEvent{Type: signal.Type, Message: "Latest release temporally correlated", OccurredAt: incident.RelatedRelease.Timestamp})
			}
		}
	}
	for index := range events {
		if events[index].OccurredAt == "" {
			events[index].OccurredAt = incident.ObservedAt
		}
	}
	return events
}
