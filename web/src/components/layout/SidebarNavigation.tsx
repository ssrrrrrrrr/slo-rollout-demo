import {
  Activity,
  CheckCircle2,
  ClipboardCheck,
  Database,
  GitBranch,
  LockKeyhole,
  Network,
  Settings,
  ShieldCheck,
} from "lucide-react"
import {
  platformRoutes,
  workspaceRoutes,
  type PortalRoute,
} from "@/components/layout/portalRoutes"

const systemItems = [
  {
    label: "Environments",
    icon: Database,
  },
  {
    label: "Settings",
    icon: Settings,
  },
]

const routeIcon: Record<PortalRoute, typeof ShieldCheck> = {
  Overview: Activity,
  Services: Network,
  Releases: GitBranch,
  Evidence: Database,
  Policy: ShieldCheck,
  "Supply Chain": LockKeyhole,
  "Agent Trace": Activity,
  Approval: ClipboardCheck,
  Environment: Network,
}

function RouteButton({
  route,
  activeRoute,
  onRouteChange,
}: {
  route: (typeof platformRoutes | typeof workspaceRoutes)[number]
  activeRoute: PortalRoute
  onRouteChange: (route: PortalRoute) => void
}) {
  const Icon = routeIcon[route.id]
  const active = activeRoute === route.id

  return (
    <button
      type="button"
      onClick={() => onRouteChange(route.id)}
      className={`flex w-full items-center justify-between rounded-xl border px-3 py-2.5 text-left transition ${
        active
          ? "border-[#35517a] bg-[#14233a] text-slate-50"
          : "border-transparent text-slate-400 hover:bg-[#0d1623] hover:text-slate-200"
      }`}
    >
      <span className="flex min-w-0 items-center gap-3">
        <Icon className="h-4 w-4 shrink-0" />
        <span className="min-w-0">
          <span className="block truncate text-sm font-semibold">
            {route.title}
          </span>
          <span className={`block truncate text-[11px] ${active ? "text-slate-400" : "text-slate-600"}`}>
            {route.description}
          </span>
        </span>
      </span>

      {active ? (
        <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-[#5d8fd8]" />
      ) : null}
    </button>
  )
}

export function SidebarNavigation({
  activeRoute,
  onRouteChange,
}: {
  activeRoute: PortalRoute
  onRouteChange: (route: PortalRoute) => void
}) {
  return (
    <aside className="hidden min-h-screen border-r border-[#1a2535] bg-[#080d15] lg:flex lg:flex-col">
      <div className="border-b border-[#1a2535] bg-[#080d15] px-3.5 py-4">
        <div className="rounded-2xl border border-[#223149] bg-[#eaf1f8] p-3 shadow-sm shadow-black/20">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-[#c6d4e5] bg-white shadow-sm">
              <img
                src="/brand/s-sentinel-logo.svg"
                alt="S Sentinel logo"
                className="h-6.5 w-6.5 object-contain"
              />
            </div>

            <div className="min-w-0 leading-tight">
              <p className="truncate text-[15px] font-bold tracking-tight text-[#071a33]">
                S Sentinel
              </p>
              <p className="mt-1 truncate text-xs font-semibold text-[#64748b]">
                Control Plane
              </p>
            </div>
          </div>
        </div>
      </div>

      <nav className="flex flex-1 flex-col gap-6 px-3 py-5">
        <div>
          <p className="px-3 text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">
            Platform
          </p>

          <div className="mt-2 space-y-1">
            {platformRoutes.map((route) => (
              <RouteButton
                key={route.id}
                route={route}
                activeRoute={activeRoute}
                onRouteChange={onRouteChange}
              />
            ))}
          </div>
        </div>

        <div>
          <p className="px-3 text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">
            Workspaces
          </p>

          <div className="mt-2 space-y-1">
            {workspaceRoutes.map((route) => (
              <RouteButton
                key={route.id}
                route={route}
                activeRoute={activeRoute}
                onRouteChange={onRouteChange}
              />
            ))}
          </div>
        </div>

        <div className="mt-auto">
          <p className="px-3 text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">
            System
          </p>

          <div className="mt-2 space-y-1">
            {systemItems.map((item) => {
              const Icon = item.icon

              return (
                <button
                  key={item.label}
                  type="button"
                  className="flex w-full items-center gap-3 rounded-xl border border-transparent px-3 py-2.5 text-left text-slate-500 transition hover:bg-[#0d1623] hover:text-slate-300"
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  <span className="truncate text-sm font-semibold">
                    {item.label}
                  </span>
                </button>
              )
            })}
          </div>

          <div className="mt-4 rounded-xl border border-[#1f2b3d] bg-[#0b121d] px-3 py-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-xs font-semibold text-slate-300">
                  Read-only boundary
                </p>
                <p className="mt-1 text-[11px] leading-4 text-slate-600">
                  Advisor cannot execute actions directly.
                </p>
              </div>
              <span className="h-2 w-2 shrink-0 rounded-full bg-emerald-400" />
            </div>
          </div>
        </div>
      </nav>
    </aside>
  )
}



