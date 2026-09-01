package main

type RecoveryActionType string

const (
	RecoveryRestartWorkload RecoveryActionType = "RESTART_WORKLOAD"
	RecoveryScaleWorkload   RecoveryActionType = "SCALE_WORKLOAD"
	RecoveryRollbackRelease RecoveryActionType = "ROLLBACK_RELEASE"
)

type Runbook struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Metadata   struct {
		Name string `yaml:"name" json:"name"`
	} `yaml:"metadata" json:"metadata"`
	Spec RunbookSpec `yaml:"spec" json:"spec"`
}
type RunbookSpec struct {
	Description string        `yaml:"description" json:"description"`
	Match       RunbookMatch  `yaml:"match" json:"match"`
	Action      RunbookAction `yaml:"action" json:"action"`
	Risk        struct {
		Level string `yaml:"level" json:"level"`
	} `yaml:"risk" json:"risk"`
	Approval struct {
		Required bool `yaml:"required" json:"required"`
	} `yaml:"approval" json:"approval"`
	Verification struct {
		RuntimeStatus RuntimeStatus `yaml:"runtimeStatus" json:"runtimeStatus"`
	} `yaml:"verification" json:"verification"`
}
type RunbookMatch struct {
	RuntimeStatus  []RuntimeStatus `yaml:"runtimeStatus" json:"runtimeStatus"`
	RequireRelease bool            `yaml:"requireRelease" json:"requireRelease"`
}
type RunbookAction struct {
	Type       RecoveryActionType       `yaml:"type" json:"type"`
	Parameters RecoveryActionParameters `yaml:"parameters" json:"parameters"`
}
type RecoveryActionParameters struct {
	Direction   string `yaml:"direction" json:"direction"`
	Step        int64  `yaml:"step" json:"step"`
	MinReplicas int64  `yaml:"minReplicas" json:"minReplicas"`
	MaxReplicas int64  `yaml:"maxReplicas" json:"maxReplicas"`
}

type RecoveryPlanStatus string

const (
	RecoveryPlanReady         RecoveryPlanStatus = "READY_FOR_APPROVAL"
	RecoveryPlanBlocked       RecoveryPlanStatus = "BLOCKED"
	RecoveryPlanNotActionable RecoveryPlanStatus = "NOT_ACTIONABLE"
)

type RecoveryVerificationStatus string

const (
	RecoveryVerificationPending    RecoveryVerificationStatus = "PENDING"
	RecoveryVerificationExecuting  RecoveryVerificationStatus = "EXECUTING"
	RecoveryVerificationRecovering RecoveryVerificationStatus = "RECOVERING"
	RecoveryVerificationRecovered  RecoveryVerificationStatus = "RECOVERED"
	RecoveryVerificationFailed     RecoveryVerificationStatus = "FAILED"
	RecoveryVerificationUnknown    RecoveryVerificationStatus = "UNKNOWN"
)

type RecoveryTarget struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}
type RecoveryPlan struct {
	ID         string             `json:"id"`
	IncidentID string             `json:"incidentId"`
	Service    string             `json:"service"`
	Status     RecoveryPlanStatus `json:"status"`
	Reason     string             `json:"reason,omitempty"`
	Diagnosis  struct {
		Category string `json:"category"`
		Reason   string `json:"reason"`
	} `json:"diagnosis"`
	Runbook        *Runbook               `json:"matchedRunbook,omitempty"`
	Action         RunbookAction          `json:"action"`
	Target         RecoveryTarget         `json:"target"`
	Risk           string                 `json:"risk,omitempty"`
	Policy         RemediationPolicy      `json:"policy"`
	Approval       RemediationApproval    `json:"approval"`
	Preflight      RemediationEligibility `json:"preflight"`
	BlockedReasons []string               `json:"blockedReasons"`
	Execution      *RecoveryExecution     `json:"execution,omitempty"`
	Verification   *RecoveryVerification  `json:"verification,omitempty"`
}
type RecoveryExecution struct {
	RequestKey       string             `json:"requestKey"`
	Status           string             `json:"status"`
	Action           RecoveryActionType `json:"action"`
	ExpectedReplicas int64              `json:"expectedReplicas,omitempty"`
	Reason           string             `json:"reason,omitempty"`
	Idempotent       bool               `json:"idempotent,omitempty"`
}

// RecoveryApprovalState is deliberately process-local in the first version.
// It is bound to the exact deterministic plan and cannot approve a later plan.
type RecoveryApprovalState struct {
	PlanID     string             `json:"planId"`
	IncidentID string             `json:"incidentId"`
	Service    string             `json:"service"`
	Action     RecoveryActionType `json:"action"`
	Target     RecoveryTarget     `json:"target"`
	Approved   bool               `json:"approved"`
	ApprovedAt string             `json:"approvedAt"`
	ApprovedBy string             `json:"approvedBy,omitempty"`
}
type RecoveryVerification struct {
	Status        RecoveryVerificationStatus `json:"status"`
	RuntimeStatus RuntimeStatus              `json:"runtimeStatus"`
	Reason        string                     `json:"reason,omitempty"`
}

type RecoveryPlanner interface {
	Plan(ReliabilityIncident, Service, []Runbook) *Runbook
}
type DeterministicRecoveryPlanner struct{}
