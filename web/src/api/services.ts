import { fetchJson } from "@/api/client"
import type { ServiceRuntimeResponse, ServiceSLOResponse, ServicesResponse } from "@/types/service"

export function fetchServices() {
  return fetchJson<ServicesResponse>("/api/v1/services")
}

export function fetchServiceSLO(name: string) {
  return fetchJson<ServiceSLOResponse>(`/api/v1/services/${encodeURIComponent(name)}/slo`)
}

export function fetchServiceRuntime(name: string) {
  return fetchJson<ServiceRuntimeResponse>(`/api/v1/services/${encodeURIComponent(name)}/runtime`)
}
