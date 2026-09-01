import { fetchJson } from "@/api/client"
export type AgentDiagnosis = { category: string; summary: string; confidence: number; evidence: string[]; candidateRunbooks: { id: string; confidence: number; reason: string }[]; provider: string; fallbackUsed: boolean }
const path=(id:string)=>`/api/v1/incidents/${encodeURIComponent(id)}/analysis`
export const fetchIncidentAnalysis=(id:string)=>fetchJson<{analysis:AgentDiagnosis|null}>(path(id))
export const analyzeIncident=(id:string)=>fetchJson<{analysis:AgentDiagnosis}>(path(id),{method:"POST"})
