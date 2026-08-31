import type { IncidentCorrelation, IncidentSeverity, IncidentStatus } from "@/types/incident"
import type { RuntimeStatus, ServiceReleaseSummary, SLOStatus } from "@/types/service"

export type ReliabilityOverallStatus = "HEALTHY" | "AT_RISK" | "UNHEALTHY" | "UNKNOWN"

export type FleetSummary = {
  totalServices: number
  healthyServices: number
  atRiskServices: number
  unhealthyServices: number
  unknownServices: number
  activeIncidents: number
  sev1Incidents: number
  sev2Incidents: number
  sev3Incidents: number
  sloBreaches: number
  runtimeUnhealthy: number
  runtimeDegraded: number
  releaseRisks: number
}

export type ServiceReliabilitySummary = {
  name: string
  displayName: string
  overallStatus: ReliabilityOverallStatus
  sloStatus: SLOStatus
  runtimeStatus: RuntimeStatus
  incidentStatus?: IncidentStatus
  incidentSeverity?: IncidentSeverity
  incidentId?: string
  latestRelease: ServiceReleaseSummary | null
  errorBudgetRemaining?: number
  burnRate1h?: number
  observedAt: string
}

export type AttentionItem = {
  service: string
  priority: "CRITICAL" | "HIGH" | "MEDIUM" | string
  type: string
  title: string
  reason: string
  relatedIncident?: string
  relatedRelease?: IncidentCorrelation
  actionTarget: "INCIDENT" | "SERVICE" | string
}

export type ReliabilityOverview = {
  schemaVersion: string
  generatedAt: string
  summary: FleetSummary
  services: ServiceReliabilitySummary[]
  attention: AttentionItem[]
}
