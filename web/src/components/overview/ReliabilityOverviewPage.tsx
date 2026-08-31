import { Activity, HeartPulse, ShieldAlert, Siren, TriangleAlert } from "lucide-react"
import { ProductMetricCard } from "@/components/common/ProductMetricCard"
import { Panel } from "@/components/common/Panel"
import { RoutePageHeader } from "@/components/layout/RoutePageHeader"
import type { ReliabilityOverview, ReliabilityOverallStatus } from "@/types/overview"
import { formatTime } from "@/utils/format"

const statusClass: Record<ReliabilityOverallStatus, string> = {
  HEALTHY: "border-emerald-900/45 bg-emerald-950/20 text-emerald-200",
  AT_RISK: "border-amber-900/45 bg-amber-950/20 text-amber-200",
  UNHEALTHY: "border-rose-900/45 bg-rose-950/20 text-rose-200",
  UNKNOWN: "border-slate-700 bg-slate-900 text-slate-300",
}

function formatPercent(value: number | undefined) {
  return value === undefined ? "—" : `${value.toFixed(1).replace(/\.0$/, "")}%`
}

export function ReliabilityOverviewPage({ overview, onOpenService, onOpenIncident }: { overview: ReliabilityOverview; onOpenService: (service: string) => void; onOpenIncident: (incidentId: string) => void }) {
  const summary = overview.summary
  return (
    <>
      <RoutePageHeader eyebrow="Overview" title="Reliability Overview" description="以 Service Fleet 为中心查看当前可靠性状态，并优先处理真正需要关注的异常。" badges={[{ label: "services", value: String(summary.totalServices), tone: "info" }, { label: "active incidents", value: String(summary.activeIncidents), tone: summary.activeIncidents > 0 ? "danger" : "neutral" }]} />

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <ProductMetricCard label="Services" value={String(summary.totalServices)} icon={Activity} hint="managed fleet" />
        <ProductMetricCard label="Healthy" value={String(summary.healthyServices)} icon={HeartPulse} hint="SLO and Runtime healthy" statusValue="success" />
        <ProductMetricCard label="At Risk" value={String(summary.atRiskServices)} icon={TriangleAlert} hint="requires attention soon" statusValue="medium" />
        <ProductMetricCard label="Unhealthy" value={String(summary.unhealthyServices)} icon={ShieldAlert} hint="current reliability impact" statusValue="high" />
        <ProductMetricCard label="Active Incidents" value={String(summary.activeIncidents)} icon={Siren} hint={`SEV1 ${summary.sev1Incidents} · SEV2 ${summary.sev2Incidents} · SEV3 ${summary.sev3Incidents}`} statusValue={summary.activeIncidents > 0 ? "high" : "neutral"} />
      </div>

      <Panel className="border-amber-900/30 bg-[#101724]">
        <div className="flex items-center justify-between gap-4"><div><p className="text-xs font-semibold uppercase tracking-[0.2em] text-amber-300/70">Needs Attention</p><h3 className="mt-1 text-lg font-semibold text-slate-100">Prioritized reliability work</h3></div><span className="font-mono text-xs text-slate-500">{overview.attention.length} items</span></div>
        {overview.attention.length === 0 ? <p className="mt-4 text-sm text-slate-500">No services currently require attention.</p> : <div className="mt-4 divide-y divide-[#263246] rounded-xl border border-[#263246] bg-[#070b12]">{overview.attention.map((item) => <div key={item.service} className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"><div><div className="flex flex-wrap items-center gap-2"><span className="rounded-full border border-amber-800/60 bg-amber-950/30 px-2 py-0.5 font-mono text-[10px] font-semibold text-amber-100">{item.priority}</span><span className="font-mono text-xs text-slate-500">{item.service}</span></div><p className="mt-2 text-sm font-semibold text-slate-100">{item.title}</p><p className="mt-1 text-sm text-slate-400">{item.reason}</p></div><button type="button" onClick={() => item.actionTarget === "INCIDENT" && item.relatedIncident ? onOpenIncident(item.relatedIncident) : onOpenService(item.service)} className="shrink-0 rounded-lg border border-[#35517a] bg-[#14233a] px-3 py-2 text-sm font-semibold text-slate-100 transition hover:bg-[#1b3151]">{item.actionTarget === "INCIDENT" ? "View Incident" : "View Service"}</button></div>)}</div>}
      </Panel>

      <Panel padding="none" className="overflow-hidden">
        <div className="flex items-center justify-between px-5 py-4"><div><h3 className="text-sm font-semibold text-slate-100">Service Fleet</h3><p className="mt-1 text-xs text-slate-500">Observed {formatTime(overview.generatedAt)}</p></div><span className="text-xs text-slate-500">SLO breaches {summary.sloBreaches} · Runtime degraded {summary.runtimeDegraded} · Release risks {summary.releaseRisks}</span></div>
        <div className="overflow-x-auto"><table className="min-w-full text-left text-sm"><thead className="border-y border-[#1f2b3d] bg-[#070b12] text-[10px] uppercase tracking-[0.16em] text-slate-500"><tr><th className="px-5 py-3 font-semibold">Service</th><th className="px-3 py-3 font-semibold">Overall</th><th className="px-3 py-3 font-semibold">SLO</th><th className="px-3 py-3 font-semibold">Runtime</th><th className="px-3 py-3 font-semibold">Incident</th><th className="px-3 py-3 font-semibold">Error budget</th><th className="px-5 py-3 font-semibold">Latest release</th></tr></thead><tbody className="divide-y divide-[#1f2b3d]">{overview.services.map((service) => <tr key={service.name} onClick={() => onOpenService(service.name)} className="cursor-pointer text-slate-300 transition hover:bg-[#101a29]"><td className="px-5 py-3"><p className="font-semibold text-slate-100">{service.displayName}</p><p className="mt-0.5 font-mono text-xs text-slate-500">{service.name}</p></td><td className="px-3 py-3"><span className={`rounded-full border px-2 py-1 font-mono text-xs font-semibold ${statusClass[service.overallStatus]}`}>{service.overallStatus}</span></td><td className="px-3 py-3 font-mono text-xs">{service.sloStatus}</td><td className="px-3 py-3 font-mono text-xs">{service.runtimeStatus}</td><td className="px-3 py-3 font-mono text-xs">{service.incidentSeverity || "—"}</td><td className="px-3 py-3 font-mono text-xs">{formatPercent(service.errorBudgetRemaining)}</td><td className="px-5 py-3 font-mono text-xs">{service.latestRelease ? `${service.latestRelease.status} · ${service.latestRelease.id}` : "—"}</td></tr>)}</tbody></table></div>
      </Panel>
    </>
  )
}
