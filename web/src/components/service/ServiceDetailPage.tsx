import { ArrowRight } from "lucide-react"
import { KeyValueRows } from "@/components/common/KeyValueRows"
import { Panel } from "@/components/common/Panel"
import { Pill } from "@/components/common/Pill"
import { ServiceActiveIncidentPanel } from "@/components/incident/ServiceActiveIncidentPanel"
import { ServiceRuntimePanel } from "@/components/service/ServiceRuntimePanel"
import { ServiceSLOPanel } from "@/components/service/ServiceSLOPanel"
import type { ServiceSummary } from "@/types/service"
import { formatTime } from "@/utils/format"

export function ServiceDetailPage({
  service,
  onOpenRelease,
  onOpenIncident,
}: {
  service: ServiceSummary
  onOpenRelease: (releaseId: string) => void
  onOpenIncident: (incidentId: string) => void
}) {
  const latestRelease = service.latestRelease

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <Panel>
        <div className="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.22em] text-slate-500">Service</p>
            <h3 className="mt-2 text-2xl font-semibold tracking-tight text-slate-100">{service.displayName}</h3>
            <p className="mt-1 font-mono text-sm text-slate-500">{service.name}</p>
          </div>
          <Pill tone="info">Owner: {service.owner}</Pill>
        </div>

        <div className="mt-5 flex flex-wrap gap-2">
          {service.environments.map((environment) => (
            <Pill key={environment} tone="dark">{environment}</Pill>
          ))}
        </div>
      </Panel>

      <ServiceSLOPanel serviceName={service.name} />

      <ServiceRuntimePanel serviceName={service.name} />

      <ServiceActiveIncidentPanel serviceName={service.name} onOpenIncident={onOpenIncident} />

      <Panel>
        <h4 className="text-sm font-semibold text-slate-100">Service references</h4>
        <div className="mt-4">
          <KeyValueRows rows={[
            ["Runtime namespace", service.runtime.namespace],
            ["Runtime workload", `${service.runtime.workload.kind}/${service.runtime.workload.name}`],
            ["SLO reference", service.sloRef],
            ["Strategy reference", service.strategyRef],
          ]} />
        </div>
      </Panel>

      <Panel>
        <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <h4 className="text-sm font-semibold text-slate-100">Latest Release</h4>
            {latestRelease ? (
              <p className="mt-1 text-sm text-slate-400">
                {latestRelease.status || "unknown"} · {formatTime(latestRelease.timestamp)}
              </p>
            ) : (
              <p className="mt-1 text-sm text-slate-500">No release has been associated with this service yet.</p>
            )}
          </div>

          {latestRelease ? (
            <button
              type="button"
              onClick={() => onOpenRelease(latestRelease.id)}
              className="inline-flex items-center justify-center gap-2 rounded-lg border border-[#35517a] bg-[#14233a] px-3 py-2 text-sm font-semibold text-slate-100 transition hover:bg-[#1b3151]"
            >
              Open Release Control Room
              <ArrowRight className="h-4 w-4" />
            </button>
          ) : null}
        </div>
      </Panel>
    </div>
  )
}
