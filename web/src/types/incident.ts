import type { RuntimeSnapshot, ServiceSLOStatus } from "@/types/service"

export type IncidentStatus = "ACTIVE" | "MITIGATING" | "RECOVERING" | "RESOLVED" | "UNKNOWN"
export type IncidentSeverity = "SEV1" | "SEV2" | "SEV3" | "SEV4"

export type IncidentSignal = {
  type: string
  status: string
  reason?: string
}

export type IncidentCorrelation = {
  id: string
  status: string
  correlation: "TEMPORAL" | string
  timestamp?: string
}

export type IncidentRecommendation = {
  action: string
  source: string
}

export type IncidentReleaseEvidence = {
  policyDecision?: string
  finalAction?: string
}

export type IncidentTimelineEvent = {
  id?: string
  type: string
  message: string
  occurredAt: string
  payload?: Record<string, unknown>
}

export type ReliabilityIncident = {
  id: string
  fingerprint?: string
  service: string
  status: IncidentStatus
  severity: IncidentSeverity
  title: string
  primarySignal: IncidentSignal
  signals: IncidentSignal[]
  relatedRelease?: IncidentCorrelation
  recommendation?: IncidentRecommendation
  releaseEvidence?: IncidentReleaseEvidence
  slo: ServiceSLOStatus
  runtime: RuntimeSnapshot
  timeline: IncidentTimelineEvent[]
  startedAt: string
  observedAt: string
  firstObservedAt?: string
  lastObservedAt?: string
  mitigationStartedAt?: string
  recoveringAt?: string
  resolvedAt?: string
  createdAt?: string
  updatedAt?: string
}

export type IncidentsResponse = {
  schemaVersion: string
  generatedAt: string
  count: number
  items: ReliabilityIncident[]
}

export type IncidentResponse = {
  schemaVersion: string
  incident: ReliabilityIncident
}

export type ActiveIncidentResponse = {
  schemaVersion: string
  service: string
  incident: ReliabilityIncident | null
}

export type IncidentTimelineResponse = { schemaVersion: string; incidentId: string; items: IncidentTimelineEvent[] }
