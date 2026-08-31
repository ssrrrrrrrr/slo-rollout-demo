import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { fetchLatestRelease, fetchReleases } from "@/api/releases"
import { fetchServices } from "@/api/services"
import {
  fetchReleaseResource,
  getResourceKindByTab,
} from "@/api/releaseResources"
import { LayoutShell } from "@/components/layout/LayoutShell"
import { PortalRouteRenderer } from "@/components/layout/PortalRouteRenderer"
import { PortalState } from "@/components/layout/PortalState"
import { ServicesPage } from "@/components/service/ServicesPage"
import {
  defaultPortalRoute,
  type PortalRoute,
} from "@/components/layout/portalRoutes"
import type { ReleaseContext } from "@/components/layout/ReleaseContextBar"
import type { ServiceSummary } from "@/types/service"


function displayValue(value: unknown, fallback = "unknown") {
  if (typeof value === "string" && value.trim().length > 0) {
    return value
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value)
  }

  return fallback
}

function App() {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [selectedServiceName, setSelectedServiceName] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState("概览")
  const [activeRoute, setActiveRoute] = useState<PortalRoute>(defaultPortalRoute)

  const releasesQuery = useQuery({
    queryKey: ["releases"],
    queryFn: fetchReleases,
    refetchInterval: 15000,
  })

  const latestQuery = useQuery({
    queryKey: ["latest-release"],
    queryFn: fetchLatestRelease,
    refetchInterval: 15000,
  })

  const servicesQuery = useQuery({
    queryKey: ["services"],
    queryFn: fetchServices,
    refetchInterval: 15000,
  })

  const releases = useMemo(() => releasesQuery.data?.items ?? [], [releasesQuery.data?.items])
  const selected = releases.find((release) => release.releaseId === selectedId) ?? releases[0]
  const selectedSummary = selected?.summary
  const services = useMemo(() => servicesQuery.data?.items ?? [], [servicesQuery.data?.items])
  const selectedService = services.find((service) => service.name === selectedServiceName) ?? services[0]
  const isServicesRoute = activeRoute === "Services"
  const resourceKind = getResourceKindByTab(activeTab)

  const releaseContext = useMemo<ReleaseContext>(() => {
    if (isServicesRoute) {
      const service = selectedService as ServiceSummary | undefined
      return {
        service: service?.name ?? "no service",
        environment: service?.environments.join(", ") || "unknown",
        releaseId: service?.latestRelease?.id ?? "no release",
        version: "not reported",
        result: service?.latestRelease?.status ?? "unknown",
        imageDigest: "not reported",
      }
    }

    const release = selected as Record<string, unknown> | undefined
    const summary = selectedSummary as Record<string, unknown> | undefined

    return {
      service: displayValue(summary?.service ?? release?.service, "demo-app"),
      environment: displayValue(summary?.environment ?? summary?.env ?? release?.environment ?? release?.env, "unknown"),
      releaseId: displayValue(selected?.releaseId ?? release?.releaseId, "no release"),
      version: displayValue(summary?.version ?? summary?.targetVersion ?? release?.version, "unknown"),
      result: displayValue(summary?.releaseResult ?? summary?.result ?? release?.result, "unknown"),
      imageDigest: displayValue(summary?.imageDigest ?? release?.imageDigest ?? release?.digest, "not reported"),
    }
  }, [isServicesRoute, selected, selectedService, selectedSummary])

  const resourceQuery = useQuery({
    queryKey: ["release-resource", selected?.releaseId, resourceKind],
    queryFn: () => fetchReleaseResource(selected!.releaseId, resourceKind),
    enabled: Boolean(selected?.releaseId),
    staleTime: 10000,
  })

  const environmentEvidenceQuery = useQuery({
    queryKey: ["release-environment-evidence", selected?.releaseId],
    queryFn: () => fetchReleaseResource(selected!.releaseId, "evidence"),
    enabled: Boolean(selected?.releaseId),
    staleTime: 10000,
  })

  const isLoading = isServicesRoute
    ? servicesQuery.isLoading
    : releasesQuery.isLoading || latestQuery.isLoading
  const hasError = isServicesRoute
    ? servicesQuery.isError
    : releasesQuery.isError || latestQuery.isError

  function refreshAll() {
    void releasesQuery.refetch()
    void latestQuery.refetch()
    void servicesQuery.refetch()
    void environmentEvidenceQuery.refetch()
  }

  return (
    <LayoutShell
      hasError={hasError}
      latest={latestQuery.data}
      generatedAt={isServicesRoute ? servicesQuery.data?.generatedAt : releasesQuery.data?.generatedAt}
      activeRoute={activeRoute}
      onRouteChange={setActiveRoute}
      releaseContext={releaseContext}
      onRefresh={refreshAll}
    >
      {isLoading ? (
        <PortalState kind="loading" />
      ) : hasError ? (
        <PortalState kind="error" />
      ) : isServicesRoute ? (
        <ServicesPage
          services={services}
          selected={selectedService}
          onSelect={setSelectedServiceName}
          onOpenRelease={(releaseId) => {
            setSelectedId(releaseId)
            setActiveRoute("Releases")
          }}
        />
      ) : !selected || !selectedSummary ? (
        <PortalState kind="empty" />
      ) : (
        <PortalRouteRenderer
          activeRoute={activeRoute}
          releases={releases}
          selected={selected}
          totalCount={releasesQuery.data?.count ?? releases.length}
          onSelect={setSelectedId}
          onRefresh={refreshAll}
          activeTab={activeTab}
          onTabChange={setActiveTab}
          onRouteChange={setActiveRoute}
          latest={latestQuery.data}
          resourceKind={resourceKind}
          resourceQuery={resourceQuery}
          evidenceQuery={environmentEvidenceQuery}
          releaseCount={releases.length}
        />
      )}
    </LayoutShell>
  )
}

export default App




