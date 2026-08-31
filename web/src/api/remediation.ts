import { fetchJson } from "@/api/client"
import type { RemediationResponse, RemediationVerificationResponse } from "@/types/remediation"

const incidentPath = (incidentId: string) => `/api/v1/incidents/${encodeURIComponent(incidentId)}/remediation`

export function fetchRemediation(incidentId: string) {
  return fetchJson<RemediationResponse>(incidentPath(incidentId))
}

export function previewRemediation(incidentId: string) {
  return fetchJson<RemediationResponse>(`${incidentPath(incidentId)}/preview`, { method: "POST" })
}

export function executeRemediation(incidentId: string, action: string) {
  return fetchJson<RemediationResponse>(`${incidentPath(incidentId)}/execute`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action }),
  })
}

export function fetchRemediationVerification(incidentId: string) {
  return fetchJson<RemediationVerificationResponse>(`${incidentPath(incidentId)}/verification`)
}
