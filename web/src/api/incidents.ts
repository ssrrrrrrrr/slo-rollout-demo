import { fetchJson } from "@/api/client"
import type { ActiveIncidentResponse, IncidentResponse, IncidentsResponse, IncidentTimelineResponse } from "@/types/incident"

export function fetchIncidents(includeResolved = false) {
  return fetchJson<IncidentsResponse>(`/api/v1/incidents${includeResolved ? "?includeResolved=true" : ""}`)
}

export function fetchIncident(id: string) {
  return fetchJson<IncidentResponse>(`/api/v1/incidents/${encodeURIComponent(id)}`)
}

export function fetchIncidentTimeline(id: string) {
  return fetchJson<IncidentTimelineResponse>(`/api/v1/incidents/${encodeURIComponent(id)}/timeline`)
}

export function fetchActiveServiceIncident(serviceName: string) {
  return fetchJson<ActiveIncidentResponse>(`/api/v1/services/${encodeURIComponent(serviceName)}/incidents/active`)
}
