import { Siren } from "lucide-react"
import { Panel } from "@/components/common/Panel"
import { RoutePageHeader } from "@/components/layout/RoutePageHeader"
import { IncidentDetailPage } from "@/components/incident/IncidentDetailPage"
import type { ReliabilityIncident } from "@/types/incident"
import { formatTime } from "@/utils/format"

export function IncidentsPage({ incidents, selected, onSelect, onOpenRelease }: { incidents: ReliabilityIncident[]; selected?: ReliabilityIncident; onSelect: (id: string) => void; onOpenRelease: (releaseId: string) => void }) {
  return (
    <>
      <RoutePageHeader eyebrow="Incidents" title="Reliability Incidents" description="将当前 SLO、Runtime 与已有 Release Evidence 组合为可解释的服务可靠性事件。" badges={[{ label: "active", value: String(incidents.length), tone: incidents.length > 0 ? "danger" : "neutral" }]} />
      {incidents.length === 0 ? (
        <Panel tone="muted" padding="lg" className="text-sm">No active reliability incidents</Panel>
      ) : (
        <div className="grid gap-6 xl:grid-cols-[minmax(280px,0.75fr)_minmax(0,1.5fr)]">
          <aside className="rounded-2xl border border-[#1f2b3d] bg-[#0f1724] p-4 shadow-sm shadow-black/20">
            <div className="mb-4 flex items-center gap-2"><Siren className="h-4 w-4 text-rose-300" /><div><h3 className="font-semibold text-slate-100">Active Incidents</h3><p className="text-xs text-slate-500">Real-time reliability correlation</p></div></div>
            <div className="space-y-2">
              {incidents.map((incident) => {
                const active = incident.id === selected?.id
                return <button key={incident.id} type="button" onClick={() => onSelect(incident.id)} className={`w-full rounded-xl border p-3 text-left transition ${active ? "border-rose-800/70 bg-rose-950/25" : "border-[#1f2b3d] bg-[#0b121d] hover:border-[#35517a]"}`}>
                  <div className="flex items-center justify-between gap-2"><span className="font-mono text-xs text-rose-200">{incident.id}</span><span className="rounded-full border border-rose-900/55 px-2 py-0.5 font-mono text-[10px] font-semibold text-rose-200">{incident.severity}</span></div>
                  <p className="mt-2 text-sm font-semibold text-slate-100">{incident.service}</p><p className="mt-1 text-xs text-slate-500">{incident.primarySignal.type} · {formatTime(incident.observedAt)}</p>
                </button>
              })}
            </div>
          </aside>
          {selected ? <IncidentDetailPage incident={selected} onOpenRelease={onOpenRelease} /> : null}
        </div>
      )}
    </>
  )
}
