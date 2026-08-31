package main

type RemediationPlanStatus string

const (
	RemediationPlanActionable    RemediationPlanStatus = "ACTIONABLE"
	RemediationPlanNotActionable RemediationPlanStatus = "NOT_ACTIONABLE"
	RemediationPlanBlocked       RemediationPlanStatus = "BLOCKED"
)

type RemediationVerificationStatus string

const (
	RemediationVerificationPending    RemediationVerificationStatus = "PENDING"
	RemediationVerificationRecovering RemediationVerificationStatus = "RECOVERING"
	RemediationVerificationRecovered  RemediationVerificationStatus = "RECOVERED"
	RemediationVerificationFailed     RemediationVerificationStatus = "FAILED"
	RemediationVerificationUnknown    RemediationVerificationStatus = "UNKNOWN"
)

type RemediationPlan struct {
	IncidentID     string                   `json:"incidentId"`
	Service        string                   `json:"service"`
	Status         RemediationPlanStatus    `json:"status"`
	Reason         string                   `json:"reason,omitempty"`
	RelatedRelease *IncidentCorrelation     `json:"relatedRelease,omitempty"`
	Target         RemediationTarget        `json:"target"`
	Recommendation RemediationAction        `json:"recommendation"`
	Operation      string                   `json:"operation"`
	Policy         RemediationPolicy        `json:"policy"`
	Approval       RemediationApproval      `json:"approval"`
	Eligibility    RemediationEligibility   `json:"eligibility"`
	AllowedActions []string                 `json:"allowedActions"`
	Execution      *RemediationExecution    `json:"execution,omitempty"`
	Verification   *RemediationVerification `json:"verification,omitempty"`
}

// RemediationTarget identifies the existing Release Control target. It is not
// a new runtime target or an instruction to mutate a workload directly.
type RemediationTarget struct {
	ReleaseID string `json:"releaseId,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Workload  string `json:"workload,omitempty"`
}

type RemediationAction struct {
	Action string `json:"action"`
	Source string `json:"source,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type RemediationPolicy struct {
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type RemediationApproval struct {
	Required bool `json:"required"`
	Approved bool `json:"approved"`
}

type RemediationEligibility struct {
	Eligible        bool     `json:"eligible"`
	Reason          string   `json:"reason,omitempty"`
	BlockingReasons []string `json:"blockingReasons"`
}

type RemediationExecution struct {
	RequestKey     string                 `json:"requestKey"`
	Status         string                 `json:"status"`
	Action         string                 `json:"action"`
	ExecutedAt     string                 `json:"executedAt,omitempty"`
	StartedAt      string                 `json:"startedAt,omitempty"`
	FinishedAt     string                 `json:"finishedAt,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	Target         RemediationTarget      `json:"target"`
	PostState      map[string]interface{} `json:"postState,omitempty"`
	ResultID       string                 `json:"runtimeActionExecutionResultId,omitempty"`
	ActionVerified bool                   `json:"actionVerified"`
	Idempotent     bool                   `json:"idempotent,omitempty"`
}

type RemediationVerification struct {
	Status           RemediationVerificationStatus `json:"status"`
	ActionVerified   bool                          `json:"actionVerified"`
	RuntimeStatus    RuntimeStatus                 `json:"runtimeStatus"`
	RuntimeRecovered bool                          `json:"runtimeRecovered"`
	SLOStatus        SLOStatus                     `json:"sloStatus"`
	SLORecovered     bool                          `json:"sloRecovered"`
	BurnRate1h       *float64                      `json:"burnRate1h,omitempty"`
	Reason           string                        `json:"reason,omitempty"`
}
