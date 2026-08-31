import { fetchJson } from "@/api/client"
import type { ReliabilityOverview } from "@/types/overview"

export function fetchReliabilityOverview() {
  return fetchJson<ReliabilityOverview>("/api/v1/overview")
}
