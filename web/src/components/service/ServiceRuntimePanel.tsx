import { useQuery } from "@tanstack/react-query"
import { fetchServiceRuntime } from "@/api/services"
import { Panel } from "@/components/common/Panel"
import type { RuntimeStatus } from "@/types/service"
import { formatTime } from "@/utils/format"

const statusClass: Record<RuntimeStatus, string> = {
  HEALTHY: "border-emerald-900/45 bg-emerald-950/20 text-emerald-200",
  DEGRADED: "border-amber-900/45 bg-amber-950/20 text-amber-200",
  UNHEALTHY: "border-rose-900/45 bg-rose-950/20 text-rose-200",
  UNKNOWN: "border-slate-700 bg-slate-900/70 text-slate-300",
}

function valueOrDash(value: string | undefined) {
  return value || "—"
}

export function ServiceRuntimePanel({ serviceName }: { serviceName: string }) {
  const runtimeQuery = useQuery({
    queryKey: ["service-runtime", serviceName],
    queryFn: () => fetchServiceRuntime(serviceName),
    refetchInterval: 30000,
    staleTime: 10000,
  })

  if (runtimeQuery.isLoading) {
    return <Panel tone="muted" className="text-sm">Loading runtime status…</Panel>
  }

  if (runtimeQuery.isError || !runtimeQuery.data) {
    return <Panel tone="muted" className="text-sm">Runtime data unavailable</Panel>
  }

  const runtime = runtimeQuery.data.runtime
  const workload = runtime.workload
  if (runtime.status === "UNKNOWN") {
    return (
      <Panel tone="muted">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h4 className="text-sm font-semibold text-slate-100">Service Runtime</h4>
            <p className="mt-1 text-sm text-slate-400">Runtime data unavailable{runtime.reason ? `: ${runtime.reason}` : ""}</p>
          </div>
          <span className={`rounded-full border px-3 py-1 font-mono text-xs font-semibold ${statusClass.UNKNOWN}`}>UNKNOWN</span>
        </div>
      </Panel>
    )
  }

  return (
    <Panel>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Service Runtime</p>
          <h4 className="mt-2 text-lg font-semibold text-slate-100">{workload.kind}/{workload.name}</h4>
          <p className="mt-1 text-sm text-slate-400">{valueOrDash(runtime.namespace)} · observed {formatTime(runtime.observedAt)}</p>
        </div>
        <span className={`rounded-full border px-3 py-1 font-mono text-xs font-semibold ${statusClass[runtime.status]}`}>{runtime.status}</span>
      </div>

      <div className="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Metric title="Ready replicas" value={`${workload.readyReplicas} / ${workload.desiredReplicas}`} />
        <Metric title="Available replicas" value={`${workload.availableReplicas} / ${workload.desiredReplicas}`} />
        <Metric title="Revision" value={valueOrDash(workload.revision)} />
        <Metric title="Primary image" value={valueOrDash(runtime.primaryImage)} mono />
      </div>

      <div className="mt-5 overflow-x-auto rounded-xl border border-[#1f2b3d]">
        <table className="min-w-full text-left text-sm">
          <thead className="bg-[#070b12] text-[10px] uppercase tracking-[0.16em] text-slate-500">
            <tr>
              <th className="px-3 py-3 font-semibold">Pod</th>
              <th className="px-3 py-3 font-semibold">Phase</th>
              <th className="px-3 py-3 font-semibold">Ready</th>
              <th className="px-3 py-3 font-semibold">Restarts</th>
              <th className="px-3 py-3 font-semibold">Node</th>
              <th className="px-3 py-3 font-semibold">Image</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[#1f2b3d] text-slate-300">
            {runtime.pods.map((pod) => (
              <tr key={pod.name}>
                <td className="px-3 py-3 font-mono text-xs text-slate-100">{pod.name}</td>
                <td className="px-3 py-3">{pod.phase}</td>
                <td className="px-3 py-3">{pod.ready ? "Ready" : "Not ready"}</td>
                <td className="px-3 py-3 font-mono">{pod.restartCount}</td>
                <td className="px-3 py-3">{valueOrDash(pod.node)}</td>
                <td className="px-3 py-3 font-mono text-xs">{valueOrDash(pod.image)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}

function Metric({ title, value, mono = false }: { title: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-xl border border-[#1f2b3d] bg-[#070b12] p-3">
      <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">{title}</p>
      <p className={`mt-2 break-all text-sm font-semibold text-slate-100 ${mono ? "font-mono text-xs" : "font-mono text-xl"}`}>{value}</p>
    </div>
  )
}
