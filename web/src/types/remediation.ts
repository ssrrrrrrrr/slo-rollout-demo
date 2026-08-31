import type { RuntimeStatus, SLOStatus } from "@/types/service"

export type RemediationPlanStatus = "ACTIONABLE" | "NOT_ACTIONABLE" | "BLOCKED"
export type RemediationVerificationStatus = "PENDING" | "RECOVERING" | "RECOVERED" | "FAILED" | "UNKNOWN"

export type RemediationPlan = {
  incidentId: string
  service: string
  status: RemediationPlanStatus
  reason?: string
  target: { releaseId?: string }
  recommendation: { action: string; source?: string; reason?: string }
  operation: string
  policy: { decision?: string; reason?: string }
  approval: { required: boolean; approved: boolean }
  eligibility: { eligible: boolean; reason?: string; blockingReasons: string[] }
  allowedActions: string[]
  execution?: {
    requestKey: string
    status: string
    action: string
    executedAt?: string
    startedAt?: string
    finishedAt?: string
    reason?: string
    target: { releaseId?: string; cluster?: string; namespace?: string; workload?: string }
    postState?: Record<string, unknown>
    runtimeActionExecutionResultId?: string
    actionVerified: boolean
    idempotent?: boolean
  }
  verification?: RemediationVerification
}

export type RemediationVerification = {
  status: RemediationVerificationStatus
  actionVerified: boolean
  runtimeStatus: RuntimeStatus
  runtimeRecovered: boolean
  sloStatus: SLOStatus
  sloRecovered: boolean
  burnRate1h?: number
  reason?: string
}

export type RemediationResponse = { schemaVersion: string; remediation: RemediationPlan }
export type RemediationVerificationResponse = { schemaVersion: string; verification: RemediationVerification }
