import { Boxes } from "lucide-react"
import { Panel } from "@/components/common/Panel"
import { Pill } from "@/components/common/Pill"
import { RoutePageHeader } from "@/components/layout/RoutePageHeader"
import { ServiceDetailPage } from "@/components/service/ServiceDetailPage"
import type { ServiceSummary } from "@/types/service"

export function ServicesPage({
  services,
  selected,
  onSelect,
  onOpenRelease,
}: {
  services: ServiceSummary[]
  selected?: ServiceSummary
  onSelect: (name: string) => void
  onOpenRelease: (releaseId: string) => void
}) {
  return (
    <>
      <RoutePageHeader
        eyebrow="Services"
        title="Service Catalog"
        description="从 Service 进入运行时引用、可靠性与交付策略，再连接到既有 Release Control Room。"
        badges={[{ label: "services", value: String(services.length), tone: "info" }]}
      />

      {services.length === 0 ? (
        <Panel tone="muted" padding="lg" className="text-sm">
          No Service definitions are available. Add a <code>configs/services/*.service.yaml</code> definition to populate the catalog.
        </Panel>
      ) : (
        <div className="grid gap-6 xl:grid-cols-[minmax(260px,0.7fr)_minmax(0,1.5fr)]">
          <aside className="rounded-2xl border border-[#1f2b3d] bg-[#0f1724] p-4 shadow-sm shadow-black/20">
            <div className="mb-4 flex items-center gap-2">
              <Boxes className="h-4 w-4 text-sky-300" />
              <div>
                <h3 className="font-semibold text-slate-100">All Services</h3>
                <p className="text-xs text-slate-500">Configuration-backed catalog</p>
              </div>
            </div>
            <div className="space-y-2">
              {services.map((service) => {
                const active = service.name === selected?.name
                return (
                  <button
                    key={service.name}
                    type="button"
                    onClick={() => onSelect(service.name)}
                    className={`w-full rounded-xl border p-3 text-left transition ${
                      active
                        ? "border-[#35517a] bg-[#14233a]"
                        : "border-[#1f2b3d] bg-[#0b121d] hover:border-[#35517a] hover:bg-[#101a29]"
                    }`}
                  >
                    <p className="font-semibold text-slate-100">{service.displayName}</p>
                    <p className="mt-1 font-mono text-xs text-slate-500">{service.name}</p>
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {service.environments.map((environment) => (
                        <Pill key={environment} tone="muted">{environment}</Pill>
                      ))}
                    </div>
                  </button>
                )
              })}
            </div>
          </aside>

          {selected ? <ServiceDetailPage service={selected} onOpenRelease={onOpenRelease} /> : null}
        </div>
      )}
    </>
  )
}
