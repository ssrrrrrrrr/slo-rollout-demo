import { ArrowRight } from "lucide-react"
import { useQuery } from "@tanstack/react-query"
import { KeyValueRows } from "@/components/common/KeyValueRows"
import { Panel } from "@/components/common/Panel"
import { IncidentRemediationPanel } from "@/components/incident/IncidentRemediationPanel"
import { IncidentRecoveryPanel } from "@/components/incident/IncidentRecoveryPanel"
import { IncidentAnalysisPanel } from "@/components/incident/IncidentAnalysisPanel"
import { fetchIncident, fetchIncidentTimeline } from "@/api/incidents"
import type { ReliabilityIncident } from "@/types/incident"
import { formatTime } from "@/utils/format"

const severityClass: Record<ReliabilityIncident["severity"], string> = {
  SEV1: "border-rose-800 bg-rose-950/45 text-rose-100", SEV2: "border-orange-800 bg-orange-950/35 text-orange-100", SEV3: "border-amber-800 bg-amber-950/35 text-amber-100", SEV4: "border-slate-700 bg-slate-900 text-slate-200",
}

export function IncidentDetailPage({ incident, onOpenRelease }: { incident: ReliabilityIncident; onOpenRelease: (releaseId: string) => void }) {
  const detail = useQuery({ queryKey: ["incident", incident.id], queryFn: () => fetchIncident(incident.id) })
  const timelineQuery = useQuery({ queryKey: ["incident-timeline", incident.id], queryFn: () => fetchIncidentTimeline(incident.id) })
  const current = detail.data?.incident ?? incident
  const timeline = timelineQuery.data?.items ?? current.timeline ?? []
  return <div className="flex min-w-0 flex-col gap-6">
    <Panel tone="danger"><div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-start"><div><p className="font-mono text-xs text-rose-200/70">{current.id}</p><h3 className="mt-2 text-xl font-semibold text-rose-50">{current.title}</h3><p className="mt-1 text-sm text-rose-100/70">Service {current.service} · first observed {formatTime(current.firstObservedAt || current.startedAt)} · last observed {formatTime(current.lastObservedAt || current.observedAt)}</p></div><div className="flex gap-2"><span className={`rounded-full border px-3 py-1 font-mono text-xs font-semibold ${severityClass[current.severity]}`}>{current.severity}</span><span className="rounded-full border border-rose-800/60 bg-rose-950/35 px-3 py-1 font-mono text-xs font-semibold text-rose-100">{current.status}</span></div></div></Panel>
    <Panel><p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">Incident lifecycle</p><div className="mt-4"><KeyValueRows rows={[["Fingerprint", current.fingerprint || "not reported"], ["First observed", formatTime(current.firstObservedAt || current.startedAt)], ["Last observed", formatTime(current.lastObservedAt || current.observedAt)], ["Resolved at", current.resolvedAt ? formatTime(current.resolvedAt) : "not resolved"]]} /></div></Panel>
    <Panel><p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">Why Sentinel opened this incident</p><h4 className="mt-2 text-lg font-semibold text-slate-100">{current.primarySignal.type}</h4><p className="mt-1 text-sm text-slate-400">{current.primarySignal.reason || `Primary state: ${current.primarySignal.status}`}</p><div className="mt-4 flex flex-wrap gap-2">{current.signals.map(signal => <span key={`${signal.type}-${signal.status}`} className="rounded-full border border-[#35517a] bg-[#14233a] px-3 py-1 font-mono text-xs font-semibold text-slate-200">{signal.type} · {signal.status}</span>)}</div></Panel>
    <div className="grid gap-6 lg:grid-cols-2"><Panel><h4 className="text-sm font-semibold text-slate-100">SLO State</h4><div className="mt-4"><KeyValueRows rows={[["Status", current.slo.status], ["Window", current.slo.window || "not reported"], ["Reason", current.slo.reason || "not reported"]]} /></div></Panel><Panel><h4 className="text-sm font-semibold text-slate-100">Runtime State</h4><div className="mt-4"><KeyValueRows rows={[["Status", current.runtime.status], ["Workload", `${current.runtime.workload.kind || "unknown"}/${current.runtime.workload.name || "unknown"}`], ["Reason", current.runtime.reason || "not reported"]]} /></div></Panel></div>
    {current.relatedRelease ? <Panel><div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center"><div><h4 className="text-sm font-semibold text-slate-100">Related Release</h4><p className="mt-1 text-sm text-slate-400">{current.relatedRelease.id} · {current.relatedRelease.status} · {current.relatedRelease.correlation.toLowerCase()} correlation</p></div><button type="button" onClick={() => onOpenRelease(current.relatedRelease!.id)} className="inline-flex items-center justify-center gap-2 rounded-lg border border-[#35517a] bg-[#14233a] px-3 py-2 text-sm font-semibold text-slate-100"><span>Open Release Control Room</span><ArrowRight className="h-4 w-4" /></button></div></Panel> : null}
    <IncidentRemediationPanel incidentId={current.id} /><IncidentAnalysisPanel incidentId={current.id} /><IncidentRecoveryPanel incidentId={current.id} />
    <Panel><h4 className="text-sm font-semibold text-slate-100">Timeline</h4><div className="mt-4 space-y-3">{timeline.length === 0 ? <p className="text-sm text-slate-500">No persisted timeline events yet.</p> : timeline.map(event => <div key={event.id || `${event.type}-${event.occurredAt}`} className="border-l-2 border-[#35517a] pl-4"><p className="text-sm font-semibold text-slate-200">{event.message}</p><p className="mt-1 font-mono text-xs text-slate-500">{event.type} · {formatTime(event.occurredAt)}</p></div>)}</div></Panel>
  </div>
}
