import { fetchJson } from "@/api/client"
import type { ServiceSLOResponse, ServicesResponse } from "@/types/service"

export function fetchServices() {
  return fetchJson<ServicesResponse>("/api/v1/services")
}

export function fetchServiceSLO(name: string) {
  return fetchJson<ServiceSLOResponse>(`/api/v1/services/${encodeURIComponent(name)}/slo`)
}
