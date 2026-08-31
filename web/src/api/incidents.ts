import { fetchJson } from "@/api/client"
import type { ActiveIncidentResponse, IncidentResponse, IncidentsResponse } from "@/types/incident"

export function fetchIncidents() {
  return fetchJson<IncidentsResponse>("/api/v1/incidents")
}

export function fetchIncident(id: string) {
  return fetchJson<IncidentResponse>(`/api/v1/incidents/${encodeURIComponent(id)}`)
}

export function fetchActiveServiceIncident(serviceName: string) {
  return fetchJson<ActiveIncidentResponse>(`/api/v1/services/${encodeURIComponent(serviceName)}/incidents/active`)
}
