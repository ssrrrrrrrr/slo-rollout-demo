import { useQuery } from "@tanstack/react-query"
import { fetchServiceSLO } from "@/api/services"
import { Panel } from "@/components/common/Panel"
import type { SLOObjectiveStatus, SLOStatus } from "@/types/service"
import { formatTime } from "@/utils/format"

const statusClass: Record<SLOStatus, string> = {
  HEALTHY: "border-emerald-900/45 bg-emerald-950/20 text-emerald-200",
  AT_RISK: "border-amber-900/45 bg-amber-950/20 text-amber-200",
  BREACHED: "border-rose-900/45 bg-rose-950/20 text-rose-200",
  UNKNOWN: "border-slate-700 bg-slate-900/70 text-slate-300",
}

function formatValue(value: number | undefined, unit = "") {
  if (value === undefined || Number.isNaN(value)) return "—"
  const digits = Math.abs(value) < 1 ? 3 : 2
  return `${value.toFixed(digits).replace(/\.0+$/, "").replace(/(\.\d*?)0+$/, "$1")}${unit}`
}

function objectiveByType(objectives: SLOObjectiveStatus[], type: string) {
  return objectives.find((objective) => objective.type === type)
}

function ObjectiveMetric({
  title,
  objective,
}: {
  title: string
  objective?: SLOObjectiveStatus
}) {
  const unit = objective?.unit === "percent" ? "%" : objective?.unit ?? ""
  return (
    <div className="rounded-xl border border-[#1f2b3d] bg-[#070b12] p-3">
      <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">{title}</p>
      <p className="mt-2 font-mono text-xl font-semibold text-slate-100">{formatValue(objective?.current, unit)}</p>
      <p className="mt-1 text-xs text-slate-500">target {formatValue(objective?.target, unit)}</p>
    </div>
  )
}

export function ServiceSLOPanel({ serviceName }: { serviceName: string }) {
  const sloQuery = useQuery({
    queryKey: ["service-slo", serviceName],
    queryFn: () => fetchServiceSLO(serviceName),
    refetchInterval: 30000,
    staleTime: 10000,
  })

  if (sloQuery.isLoading) {
    return <Panel tone="muted" className="text-sm">Loading Service SLO status…</Panel>
  }

  if (sloQuery.isError || !sloQuery.data) {
    return <Panel tone="muted" className="text-sm">SLO data unavailable</Panel>
  }

  const slo = sloQuery.data.slo
  if (slo.status === "UNKNOWN") {
    return (
      <Panel tone="muted">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h4 className="text-sm font-semibold text-slate-100">Service SLO</h4>
            <p className="mt-1 text-sm text-slate-400">SLO data unavailable{slo.reason ? `: ${slo.reason}` : ""}</p>
          </div>
          <span className={`rounded-full border px-3 py-1 font-mono text-xs font-semibold ${statusClass.UNKNOWN}`}>UNKNOWN</span>
        </div>
      </Panel>
    )
  }

  const availability = objectiveByType(slo.objectives, "availability")
  const errorRate = objectiveByType(slo.objectives, "error_rate")
  const latency = objectiveByType(slo.objectives, "latency")

  return (
    <Panel>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Service Reliability</p>
          <h4 className="mt-2 text-lg font-semibold text-slate-100">Overall SLO Status</h4>
          <p className="mt-1 text-sm text-slate-400">{slo.window} evaluation window · evaluated {formatTime(slo.evaluatedAt)}</p>
        </div>
        <span className={`rounded-full border px-3 py-1 font-mono text-xs font-semibold ${statusClass[slo.status]}`}>{slo.status}</span>
      </div>

      <div className="mt-5 grid gap-3 md:grid-cols-3">
        <ObjectiveMetric title="Availability" objective={availability} />
        <ObjectiveMetric title="Error Rate" objective={errorRate} />
        <ObjectiveMetric title="P95 Latency" objective={latency} />
      </div>

      <div className="mt-5 grid gap-3 md:grid-cols-2">
        <div className="rounded-xl border border-[#1f2b3d] bg-[#070b12] p-4">
          <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">Error Budget Remaining</p>
          <p className="mt-2 font-mono text-2xl font-semibold text-slate-100">{formatValue(slo.errorBudget.remainingPercent, "%")}</p>
          <p className="mt-1 text-xs text-slate-500">consumed {formatValue(slo.errorBudget.consumedPercent, "%")}</p>
        </div>
        <div className="rounded-xl border border-[#1f2b3d] bg-[#070b12] p-4">
          <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">Burn Rate</p>
          <div className="mt-2 grid grid-cols-3 gap-2 font-mono text-sm font-semibold text-slate-100">
            <span>1h {formatValue(slo.burnRate["1h"], "x")}</span>
            <span>6h {formatValue(slo.burnRate["6h"], "x")}</span>
            <span>24h {formatValue(slo.burnRate["24h"], "x")}</span>
          </div>
        </div>
      </div>
    </Panel>
  )
}
