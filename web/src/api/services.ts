import { fetchJson } from "@/api/client"
import type { ServicesResponse } from "@/types/service"

export function fetchServices() {
  return fetchJson<ServicesResponse>("/api/v1/services")
}
