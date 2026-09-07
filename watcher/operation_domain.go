package main

import "time"

// ControlledOperation is the internal, canonical execution envelope. It is
// deliberately a projection: RemediationPlan and RecoveryPlan remain the
// public workflow models and retain their own domain-specific fields.
type OperationSubject struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type OperationSource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type OperationAction string

const (
	OperationPauseRelease    OperationAction = "PAUSE_RELEASE"
	OperationResumeRelease   OperationAction = "RESUME_RELEASE"
	OperationPromoteRelease  OperationAction = "PROMOTE_RELEASE"
	OperationAbortRelease    OperationAction = "ABORT_RELEASE"
	OperationRollbackRelease OperationAction = "ROLLBACK_RELEASE"
	OperationRestartWorkload OperationAction = "RESTART_WORKLOAD"
	OperationScaleWorkload   OperationAction = "SCALE_WORKLOAD"
)

type OperationTarget struct {
	ReleaseID    string `json:"releaseId,omitempty"`
	Service      string `json:"service,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	WorkloadKind string `json:"workloadKind,omitempty"`
	WorkloadName string `json:"workloadName,omitempty"`
}

type OperationPolicyState struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Source   string `json:"source,omitempty"`
}

type OperationApprovalCheck struct {
	Type       string `json:"type"`
	SubjectID  string `json:"subjectId"`
	Required   bool   `json:"required"`
	Approved   bool   `json:"approved"`
	ApprovedAt string `json:"approvedAt,omitempty"`
	ApprovedBy string `json:"approvedBy,omitempty"`
}

type OperationApprovalState struct {
	Required       bool                     `json:"required"`
	Approved       bool                     `json:"approved"`
	SubjectType    string                   `json:"subjectType"`
	SubjectID      string                   `json:"subjectId"`
	RequiredChecks []OperationApprovalCheck `json:"requiredChecks"`
}

type OperationPreflightState struct {
	Status         string   `json:"status"`
	Checks         []string `json:"checks"`
	BlockedReasons []string `json:"blockedReasons"`
}

type OperationExecutionState struct {
	Status           string `json:"status"`
	Executor         string `json:"executor,omitempty"`
	StartedAt        string `json:"startedAt,omitempty"`
	FinishedAt       string `json:"finishedAt,omitempty"`
	Reason           string `json:"reason,omitempty"`
	ExternalResultID string `json:"externalResultId,omitempty"`
}

// OperationLifecycleState is the durable lifecycle of a controlled operation.
// It is intentionally distinct from the existing execution projection so a
// process restart can recover its state without inferring intent from an
// executor result.
type OperationLifecycleState string

const (
	OperationStatePlanned         OperationLifecycleState = "PLANNED"
	OperationStateWaitingApproval OperationLifecycleState = "WAITING_APPROVAL"
	OperationStateReady           OperationLifecycleState = "READY"
	OperationStateExecuting       OperationLifecycleState = "EXECUTING"
	OperationStateSucceeded       OperationLifecycleState = "SUCCEEDED"
	OperationStateVerifying       OperationLifecycleState = "VERIFYING"
	OperationStateRecovering      OperationLifecycleState = "RECOVERING"
	OperationStateRecovered       OperationLifecycleState = "RECOVERED"
	OperationStateBlocked         OperationLifecycleState = "BLOCKED"
	OperationStateFailed          OperationLifecycleState = "FAILED"
	OperationStateUnknown         OperationLifecycleState = "UNKNOWN"
)

// OperationExecutionIntent is the immutable description of the mutation an
// operation is prepared to make. It is created before READY/EXECUTING and is
// persisted independently from execution and verification summaries.
type OperationExecutionIntent struct {
	Action                OperationAction `json:"action"`
	Target                OperationTarget `json:"target"`
	ReleaseID             string          `json:"releaseId,omitempty"`
	RuntimeActionIdentity string          `json:"runtimeActionIdentity,omitempty"`
	RestartAt             string          `json:"restartAt,omitempty"`
	TargetReplicas        *int64          `json:"targetReplicas,omitempty"`
}

type OperationVerificationState struct {
	Status         string        `json:"status"`
	Reason         string        `json:"reason,omitempty"`
	RuntimeStatus  RuntimeStatus `json:"runtimeStatus,omitempty"`
	SLOStatus      SLOStatus     `json:"sloStatus,omitempty"`
	ActionVerified bool          `json:"actionVerified"`
}

type ControlledOperation struct {
	ID              string                     `json:"id"`
	Subject         OperationSubject           `json:"subject"`
	Source          OperationSource            `json:"source"`
	Action          OperationAction            `json:"action"`
	Target          OperationTarget            `json:"target"`
	Parameters      RecoveryActionParameters   `json:"parameters,omitempty"`
	Risk            string                     `json:"risk,omitempty"`
	Policy          OperationPolicyState       `json:"policy"`
	Approval        OperationApprovalState     `json:"approval"`
	Preflight       OperationPreflightState    `json:"preflight"`
	ExecutionIntent OperationExecutionIntent   `json:"executionIntent"`
	State           OperationLifecycleState    `json:"state"`
	Execution       OperationExecutionState    `json:"execution"`
	Verification    OperationVerificationState `json:"verification"`
	IdempotencyKey  string                     `json:"idempotencyKey"`
	CreatedAt       time.Time                  `json:"createdAt"`
	UpdatedAt       time.Time                  `json:"updatedAt"`
	StartedAt       *time.Time                 `json:"startedAt,omitempty"`
	FinishedAt      *time.Time                 `json:"finishedAt,omitempty"`
}
