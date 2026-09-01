import { useMemo, useState } from "react"
import { Siren } from "lucide-react"
import { Panel } from "@/components/common/Panel"
import { RoutePageHeader } from "@/components/layout/RoutePageHeader"
import { IncidentDetailPage } from "@/components/incident/IncidentDetailPage"
import type { ReliabilityIncident } from "@/types/incident"
import { formatTime } from "@/utils/format"

export function IncidentsPage({ incidents, selected, onSelect, onOpenRelease }: { incidents: ReliabilityIncident[]; selected?: ReliabilityIncident; onSelect: (id: string) => void; onOpenRelease: (releaseId: string) => void }) {
  const [view, setView] = useState<"ACTIVE" | "RESOLVED">("ACTIVE")
  const items = useMemo(() => incidents.filter(item => view === "RESOLVED" ? item.status === "RESOLVED" : item.status !== "RESOLVED"), [incidents, view])
  const current = items.find(item => item.id === selected?.id) ?? items[0]
  return <>
    <RoutePageHeader eyebrow="Incidents" title="Reliability Incidents" description="Durable Service reliability episodes and their persisted lifecycle history." badges={[{ label: "active", value: String(incidents.filter(item => item.status !== "RESOLVED").length), tone: "danger" }, { label: "resolved", value: String(incidents.filter(item => item.status === "RESOLVED").length), tone: "neutral" }]} />
    <div className="mb-5 flex gap-2"><button type="button" onClick={() => setView("ACTIVE")} className={`rounded-lg border px-3 py-2 text-sm font-semibold ${view === "ACTIVE" ? "border-[#35517a] bg-[#14233a] text-slate-100" : "border-[#263246] text-slate-400"}`}>Active</button><button type="button" onClick={() => setView("RESOLVED")} className={`rounded-lg border px-3 py-2 text-sm font-semibold ${view === "RESOLVED" ? "border-[#35517a] bg-[#14233a] text-slate-100" : "border-[#263246] text-slate-400"}`}>Resolved</button></div>
    {items.length === 0 ? <Panel tone="muted" padding="lg" className="text-sm">No {view.toLowerCase()} reliability incidents</Panel> : <div className="grid gap-6 xl:grid-cols-[minmax(280px,0.75fr)_minmax(0,1.5fr)]"><aside className="rounded-2xl border border-[#1f2b3d] bg-[#0f1724] p-4 shadow-sm shadow-black/20"><div className="mb-4 flex items-center gap-2"><Siren className="h-4 w-4 text-rose-300" /><div><h3 className="font-semibold text-slate-100">{view === "ACTIVE" ? "Active Incidents" : "Resolved Incidents"}</h3><p className="text-xs text-slate-500">Persisted incident episodes</p></div></div><div className="space-y-2">{items.map(item => <button key={item.id} type="button" onClick={() => onSelect(item.id)} className={`w-full rounded-xl border p-3 text-left transition ${item.id === current?.id ? "border-rose-800/70 bg-rose-950/25" : "border-[#1f2b3d] bg-[#0b121d] hover:border-[#35517a]"}`}><div className="flex items-center justify-between gap-2"><span className="font-mono text-xs text-rose-200">{item.id}</span><span className="rounded-full border border-rose-900/55 px-2 py-0.5 font-mono text-[10px] font-semibold text-rose-200">{item.severity}</span></div><p className="mt-2 text-sm font-semibold text-slate-100">{item.service}</p><p className="mt-1 text-xs text-slate-500">{item.primarySignal.type} · {formatTime(item.lastObservedAt || item.observedAt)}</p></button>)}</div></aside>{current ? <IncidentDetailPage incident={current} onOpenRelease={onOpenRelease} /> : null}</div>}
  </>
}
