import { useQuery } from "@tanstack/react-query"
import { fetchActiveServiceIncident } from "@/api/incidents"
import { Panel } from "@/components/common/Panel"

export function ServiceActiveIncidentPanel({
  serviceName,
  onOpenIncident,
}: {
  serviceName: string
  onOpenIncident: (incidentId: string) => void
}) {
  const incidentQuery = useQuery({
    queryKey: ["service-active-incident", serviceName],
    queryFn: () => fetchActiveServiceIncident(serviceName),
    refetchInterval: 30000,
    staleTime: 10000,
  })

  if (incidentQuery.isLoading) {
    return <Panel tone="muted" className="text-sm">Loading active incident…</Panel>
  }
  if (incidentQuery.isError || !incidentQuery.data) {
    return <Panel tone="muted" className="text-sm">Incident status unavailable</Panel>
  }

  const incident = incidentQuery.data.incident
  if (!incident) {
    return (
      <Panel tone="muted" className="text-sm">
        <h4 className="font-semibold text-slate-200">Active Incident</h4>
        <p className="mt-1 text-slate-500">No active incident</p>
      </Panel>
    )
  }

  return (
    <Panel tone="danger">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-rose-300/70">Active Incident</p>
          <h4 className="mt-1 font-semibold text-rose-100">{incident.severity} · {incident.primarySignal.type}</h4>
          <p className="mt-1 text-sm text-rose-200/75">{incident.title}</p>
        </div>
        <button
          type="button"
          onClick={() => onOpenIncident(incident.id)}
          className="rounded-lg border border-rose-800/60 bg-rose-950/35 px-3 py-2 text-sm font-semibold text-rose-100 transition hover:bg-rose-900/40"
        >
          Open Incident
        </button>
      </div>
    </Panel>
  )
}
