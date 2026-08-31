export type ServiceRuntimeRef = {
  namespace: string
  workload: {
    kind: string
    name: string
  }
}

export type ServiceReleaseSummary = {
  id: string
  status: string
  timestamp: string
}

export type ServiceSummary = {
  name: string
  displayName: string
  owner: string
  environments: string[]
  runtime: ServiceRuntimeRef
  sloRef: string
  strategyRef: string
  latestRelease: ServiceReleaseSummary | null
}

export type ServicesResponse = {
  schemaVersion: string
  generatedAt: string
  count: number
  items: ServiceSummary[]
}

export type SLOStatus = "HEALTHY" | "AT_RISK" | "BREACHED" | "UNKNOWN"

export type SLOObjectiveStatus = {
  name: string
  type: "availability" | "error_rate" | "latency" | string
  target: number
  current?: number
  unit?: string
  status: SLOStatus
  reason?: string
}

export type ErrorBudgetStatus = {
  remainingPercent?: number
  consumedPercent?: number
  status: SLOStatus
  reason?: string
}

export type BurnRateStatus = {
  "1h"?: number
  "6h"?: number
  "24h"?: number
  status: SLOStatus
  reason?: string
}

export type ServiceSLOStatus = {
  service: string
  status: SLOStatus
  window?: string
  objectives: SLOObjectiveStatus[]
  errorBudget: ErrorBudgetStatus
  burnRate: BurnRateStatus
  evaluatedAt: string
  reason?: string
}

export type ServiceSLOResponse = {
  schemaVersion: string
  slo: ServiceSLOStatus
}

export type RuntimeStatus = "HEALTHY" | "DEGRADED" | "UNHEALTHY" | "UNKNOWN"

export type RuntimeWorkloadStatus = {
  kind?: string
  name?: string
  phase?: string
  revision?: string
  desiredReplicas: number
  readyReplicas: number
  availableReplicas: number
  updatedReplicas: number
}

export type RuntimeContainerStatus = {
  name: string
  ready: boolean
  restartCount: number
  image?: string
}

export type RuntimePodStatus = {
  name: string
  phase: string
  ready: boolean
  restartCount: number
  node?: string
  image?: string
  createdAt?: string
  containers?: RuntimeContainerStatus[]
}

export type RuntimeSnapshot = {
  service: string
  status: RuntimeStatus
  namespace?: string
  workload: RuntimeWorkloadStatus
  primaryImage?: string
  images?: string[]
  pods: RuntimePodStatus[]
  observedAt: string
  reason?: string
}

export type ServiceRuntimeResponse = {
  schemaVersion: string
  runtime: RuntimeSnapshot
}
