import { fetchJson } from "@/api/client"

export type RecoveryPlan = { id: string; status: "READY_FOR_APPROVAL" | "BLOCKED" | "NOT_ACTIONABLE"; reason?: string; diagnosis: { category: string; reason: string }; matchedRunbook?: { metadata: { name: string } }; action: { type: string }; target: { namespace: string; kind: string; name: string }; risk?: string; policy: { decision?: string }; approval: { required: boolean; approved: boolean }; preflight: { eligible: boolean; blockingReasons: string[] }; execution?: { status: string }; verification?: { status: string; reason?: string } }
type RecoveryResponse = { recovery: RecoveryPlan }
const path = (id: string) => `/api/v1/incidents/${encodeURIComponent(id)}/recovery`
export const fetchRecovery = (id: string) => fetchJson<RecoveryResponse>(path(id))
export const previewRecovery = (id: string) => fetchJson<RecoveryResponse>(`${path(id)}/preview`, { method: "POST" })
export const approveRecovery = (id: string, planId: string) => fetchJson<RecoveryResponse>(`${path(id)}/approve`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ planId }) })
export const executeRecovery = (id: string, planId: string) => fetchJson<RecoveryResponse>(`${path(id)}/execute`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ planId }) })
