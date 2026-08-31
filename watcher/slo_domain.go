package main

import "time"

type SLOStatus string

const (
	SLOStatusHealthy  SLOStatus = "HEALTHY"
	SLOStatusAtRisk   SLOStatus = "AT_RISK"
	SLOStatusBreached SLOStatus = "BREACHED"
	SLOStatusUnknown  SLOStatus = "UNKNOWN"
)

// ServiceSLOStatus is a service-level reliability evaluation. It deliberately
// describes a long-lived service window rather than a single release analysis.
type ServiceSLOStatus struct {
	Service     string               `json:"service"`
	Status      SLOStatus            `json:"status"`
	Window      string               `json:"window,omitempty"`
	Objectives  []SLOObjectiveStatus `json:"objectives"`
	ErrorBudget ErrorBudgetStatus    `json:"errorBudget"`
	BurnRate    BurnRateStatus       `json:"burnRate"`
	EvaluatedAt string               `json:"evaluatedAt"`
	Reason      string               `json:"reason,omitempty"`
}

type SLOObjectiveStatus struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Target  float64   `json:"target"`
	Current *float64  `json:"current,omitempty"`
	Unit    string    `json:"unit,omitempty"`
	Status  SLOStatus `json:"status"`
	Reason  string    `json:"reason,omitempty"`
}

type ErrorBudgetStatus struct {
	RemainingPercent *float64  `json:"remainingPercent,omitempty"`
	ConsumedPercent  *float64  `json:"consumedPercent,omitempty"`
	Status           SLOStatus `json:"status"`
	Reason           string    `json:"reason,omitempty"`
}

type BurnRateStatus struct {
	OneHour     *float64  `json:"1h,omitempty"`
	SixHours    *float64  `json:"6h,omitempty"`
	TwentyFourH *float64  `json:"24h,omitempty"`
	Status      SLOStatus `json:"status"`
	Reason      string    `json:"reason,omitempty"`
}

func newUnknownServiceSLOStatus(service, reason string) ServiceSLOStatus {
	return ServiceSLOStatus{
		Service:     service,
		Status:      SLOStatusUnknown,
		Objectives:  []SLOObjectiveStatus{},
		ErrorBudget: ErrorBudgetStatus{Status: SLOStatusUnknown, Reason: reason},
		BurnRate:    BurnRateStatus{Status: SLOStatusUnknown, Reason: reason},
		EvaluatedAt: time.Now().Format(time.RFC3339),
		Reason:      reason,
	}
}
