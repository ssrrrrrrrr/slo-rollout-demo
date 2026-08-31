package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type PrometheusSLOProvider struct {
	baseURL string
	client  *http.Client
}

func NewPrometheusSLOProvider(baseURL string) *PrometheusSLOProvider {
	return &PrometheusSLOProvider{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (provider *PrometheusSLOProvider) Evaluate(ctx context.Context, service Service, config ServiceSLOConfig) (ServiceSLOStatus, error) {
	if provider.baseURL == "" {
		return ServiceSLOStatus{}, fmt.Errorf("Prometheus URL is not configured")
	}

	window := strings.TrimSpace(config.Spec.ServiceLevel.Window)
	availabilityTarget := config.Spec.ServiceLevel.AvailabilityTarget
	latencyTarget, hasLatencyTarget := config.latencyTargetMilliseconds()
	requestMetric := strings.TrimSpace(config.Spec.Observability.Prometheus.RequestCounter)
	latencyMetric := strings.TrimSpace(config.Spec.Observability.Prometheus.LatencyHistogram)
	if window == "" || availabilityTarget <= 0 || availabilityTarget > 100 || !hasLatencyTarget || requestMetric == "" || latencyMetric == "" {
		return unknownSLOStatusForConfig(service.Metadata.Name, config, "SLO config is missing long-window, availability, metric, or latency objective fields"), nil
	}

	badEvents, err := provider.query(ctx, prometheusEventCountQuery(service, config, window, true))
	if err != nil {
		return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("Prometheus bad-event query unavailable: %v", err)), nil
	}
	totalEvents, err := provider.query(ctx, prometheusEventCountQuery(service, config, window, false))
	if err != nil {
		return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("Prometheus total-event query unavailable: %v", err)), nil
	}
	if totalEvents <= 0 {
		return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("no request traffic in the %s evaluation window", window)), nil
	}
	badRate := badEvents / totalEvents
	latencySeconds, err := provider.query(ctx, prometheusP95LatencyQuery(service, config, window))
	if err != nil {
		return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("Prometheus latency query unavailable: %v", err)), nil
	}

	burnRates := make([]float64, 3)
	for index, burnWindow := range []string{"1h", "6h", "24h"} {
		burnBadEvents, err := provider.query(ctx, prometheusEventCountQuery(service, config, burnWindow, true))
		if err != nil {
			return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("Prometheus %s bad-event query unavailable: %v", burnWindow, err)), nil
		}
		burnTotalEvents, err := provider.query(ctx, prometheusEventCountQuery(service, config, burnWindow, false))
		if err != nil {
			return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("Prometheus %s total-event query unavailable: %v", burnWindow, err)), nil
		}
		if burnTotalEvents <= 0 {
			return unknownSLOStatusForConfig(service.Metadata.Name, config, fmt.Sprintf("no request traffic in the %s burn-rate window", burnWindow)), nil
		}
		burnRates[index] = calculateBurnRate(burnBadEvents/burnTotalEvents, availabilityTarget)
	}

	return calculateServiceSLOStatus(service.Metadata.Name, window, availabilityTarget, latencyTarget, badRate, latencySeconds*1000, badEvents, totalEvents, burnRates[0], burnRates[1], burnRates[2]), nil
}

func (provider *PrometheusSLOProvider) query(ctx context.Context, promQL string) (float64, error) {
	endpoint := provider.baseURL + "/api/v1/query"
	query := url.Values{}
	query.Set("query", promQL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return 0, err
	}
	response, err := provider.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("Prometheus returned HTTP %d", response.StatusCode)
	}

	var payload struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
		Error     string `json:"error"`
		Data      struct {
			Result []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode Prometheus response: %w", err)
	}
	if payload.Status != "success" {
		return 0, fmt.Errorf("Prometheus query failed: %s %s", payload.ErrorType, payload.Error)
	}
	if len(payload.Data.Result) != 1 || len(payload.Data.Result[0].Value) != 2 {
		return 0, fmt.Errorf("Prometheus returned no usable series")
	}

	value, ok := payload.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("Prometheus returned a non-string sample")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("Prometheus returned an invalid sample %q", value)
	}

	return parsed, nil
}

func prometheusEventCountQuery(service Service, config ServiceSLOConfig, window string, errorsOnly bool) string {
	prometheus := config.Spec.Observability.Prometheus
	namespaceLabel := defaultPrometheusLabel(prometheus.Labels.Namespace, "namespace")
	statusLabel := defaultPrometheusLabel(prometheus.Labels.Status, "status")
	selector := fmt.Sprintf(`%s=%q`, namespaceLabel, service.Runtime.Namespace)
	if errorsOnly {
		selector += fmt.Sprintf(`,%s=~%q`, statusLabel, defaultPrometheusLabel(prometheus.ErrorStatusRegex, "5.."))
	}

	return fmt.Sprintf(`sum(increase(%s{%s}[%s]))`, prometheus.RequestCounter, selector, window)
}

func prometheusP95LatencyQuery(service Service, config ServiceSLOConfig, window string) string {
	prometheus := config.Spec.Observability.Prometheus
	namespaceLabel := defaultPrometheusLabel(prometheus.Labels.Namespace, "namespace")
	selector := fmt.Sprintf(`%s=%q`, namespaceLabel, service.Runtime.Namespace)

	return fmt.Sprintf(`histogram_quantile(0.95, sum(rate(%s{%s}[%s])) by (le))`, prometheus.LatencyHistogram, selector, window)
}

func defaultPrometheusLabel(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func calculateServiceSLOStatus(service, window string, availabilityTarget, latencyTarget, badRate, latencyMs, badEvents, totalEvents, burnOneHour, burnSixHours, burnTwentyFourHours float64) ServiceSLOStatus {
	availability := clampPercent((1 - badRate) * 100)
	errorRatePercent := clampPercent(badRate * 100)
	allowedBadRatePercent := 100 - availabilityTarget
	consumed := calculateErrorBudgetConsumedPercent(badEvents, totalEvents, availabilityTarget)
	remaining := 100 - consumed

	availabilityStatus := statusForThreshold(availability, availabilityTarget, true)
	errorRateStatus := statusForThreshold(errorRatePercent, allowedBadRatePercent, false)
	latencyStatus := statusForThreshold(latencyMs, latencyTarget, false)
	budgetStatus := statusForErrorBudget(consumed)
	burnStatus := statusForBurnRate(burnOneHour, burnSixHours, burnTwentyFourHours)
	overallStatus := highestSLOStatus(availabilityStatus, errorRateStatus, latencyStatus, budgetStatus, burnStatus)

	return ServiceSLOStatus{
		Service: service,
		Status:  overallStatus,
		Window:  window,
		Objectives: []SLOObjectiveStatus{
			{Name: "availability", Type: "availability", Target: availabilityTarget, Current: float64Pointer(availability), Unit: "percent", Status: availabilityStatus},
			{Name: "error-rate", Type: "error_rate", Target: allowedBadRatePercent, Current: float64Pointer(errorRatePercent), Unit: "percent", Status: errorRateStatus},
			{Name: "p95-latency", Type: "latency", Target: latencyTarget, Current: float64Pointer(latencyMs), Unit: "ms", Status: latencyStatus},
		},
		ErrorBudget: ErrorBudgetStatus{
			RemainingPercent: float64Pointer(remaining),
			ConsumedPercent:  float64Pointer(consumed),
			Status:           budgetStatus,
		},
		BurnRate: BurnRateStatus{
			OneHour:     float64Pointer(burnOneHour),
			SixHours:    float64Pointer(burnSixHours),
			TwentyFourH: float64Pointer(burnTwentyFourHours),
			Status:      burnStatus,
		},
		EvaluatedAt: time.Now().Format(time.RFC3339),
		Reason:      sloStatusReason(overallStatus),
	}
}

func unknownSLOStatusForConfig(service string, config ServiceSLOConfig, reason string) ServiceSLOStatus {
	status := newUnknownServiceSLOStatus(service, reason)
	status.Window = config.Spec.ServiceLevel.Window
	availabilityTarget := config.Spec.ServiceLevel.AvailabilityTarget
	latencyTarget, hasLatencyTarget := config.latencyTargetMilliseconds()
	if availabilityTarget > 0 {
		status.Objectives = append(status.Objectives,
			SLOObjectiveStatus{Name: "availability", Type: "availability", Target: availabilityTarget, Unit: "percent", Status: SLOStatusUnknown, Reason: reason},
			SLOObjectiveStatus{Name: "error-rate", Type: "error_rate", Target: 100 - availabilityTarget, Unit: "percent", Status: SLOStatusUnknown, Reason: reason},
		)
	}
	if hasLatencyTarget {
		status.Objectives = append(status.Objectives, SLOObjectiveStatus{Name: "p95-latency", Type: "latency", Target: latencyTarget, Unit: "ms", Status: SLOStatusUnknown, Reason: reason})
	}
	return status
}

func calculateErrorBudgetConsumedPercent(badEvents, totalEvents, availabilityTarget float64) float64 {
	allowedBadRate := (100 - availabilityTarget) / 100
	if allowedBadRate <= 0 || totalEvents <= 0 {
		return 100
	}
	return clampPercent(badEvents / (totalEvents * allowedBadRate) * 100)
}

func calculateBurnRate(observedBadRate, availabilityTarget float64) float64 {
	allowedBadRate := (100 - availabilityTarget) / 100
	if allowedBadRate <= 0 {
		return 0
	}
	return math.Max(0, observedBadRate/allowedBadRate)
}

func statusForThreshold(current, target float64, higherIsBetter bool) SLOStatus {
	if higherIsBetter {
		if current >= target {
			return SLOStatusHealthy
		}
		return SLOStatusBreached
	}
	if current <= target {
		return SLOStatusHealthy
	}
	return SLOStatusBreached
}

func statusForErrorBudget(consumedPercent float64) SLOStatus {
	switch {
	case consumedPercent >= 100:
		return SLOStatusBreached
	case consumedPercent >= 80:
		return SLOStatusAtRisk
	default:
		return SLOStatusHealthy
	}
}

func statusForBurnRate(values ...float64) SLOStatus {
	status := SLOStatusHealthy
	for _, value := range values {
		switch {
		case value >= 2:
			return SLOStatusBreached
		case value >= 1:
			status = SLOStatusAtRisk
		}
	}
	return status
}

func highestSLOStatus(statuses ...SLOStatus) SLOStatus {
	for _, status := range statuses {
		if status == SLOStatusBreached {
			return SLOStatusBreached
		}
	}
	for _, status := range statuses {
		if status == SLOStatusAtRisk {
			return SLOStatusAtRisk
		}
	}
	return SLOStatusHealthy
}

func sloStatusReason(status SLOStatus) string {
	switch status {
	case SLOStatusBreached:
		return "one or more service reliability thresholds are breached"
	case SLOStatusAtRisk:
		return "service error budget or burn rate is at risk"
	default:
		return "service reliability objectives are healthy"
	}
}

func clampPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func float64Pointer(value float64) *float64 {
	return &value
}
