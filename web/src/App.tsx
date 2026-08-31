import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { fetchLatestRelease, fetchReleases } from "@/api/releases"
import { fetchServices } from "@/api/services"
import { fetchIncidents } from "@/api/incidents"
import { fetchReliabilityOverview } from "@/api/overview"
import {
  fetchReleaseResource,
  getResourceKindByTab,
} from "@/api/releaseResources"
import { LayoutShell } from "@/components/layout/LayoutShell"
import { PortalRouteRenderer } from "@/components/layout/PortalRouteRenderer"
import { PortalState } from "@/components/layout/PortalState"
import { ServicesPage } from "@/components/service/ServicesPage"
import { IncidentsPage } from "@/components/incident/IncidentsPage"
import { ReliabilityOverviewPage } from "@/components/overview/ReliabilityOverviewPage"
import {
  defaultPortalRoute,
  type PortalRoute,
} from "@/components/layout/portalRoutes"
import type { ReleaseContext } from "@/components/layout/ReleaseContextBar"
import type { ServiceSummary } from "@/types/service"
import type { ReliabilityIncident } from "@/types/incident"
import type { ReliabilityOverview } from "@/types/overview"


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
  const [selectedIncidentId, setSelectedIncidentId] = useState<string | null>(null)
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

  const incidentsQuery = useQuery({
    queryKey: ["incidents"],
    queryFn: fetchIncidents,
    refetchInterval: 15000,
  })

  const overviewQuery = useQuery({
    queryKey: ["reliability-overview"],
    queryFn: fetchReliabilityOverview,
    refetchInterval: 15000,
  })

  const releases = useMemo(() => releasesQuery.data?.items ?? [], [releasesQuery.data?.items])
  const selected = releases.find((release) => release.releaseId === selectedId) ?? releases[0]
  const selectedSummary = selected?.summary
  const services = useMemo(() => servicesQuery.data?.items ?? [], [servicesQuery.data?.items])
  const selectedService = services.find((service) => service.name === selectedServiceName) ?? services[0]
  const incidents = useMemo(() => incidentsQuery.data?.items ?? [], [incidentsQuery.data?.items])
  const selectedIncident = incidents.find((incident) => incident.id === selectedIncidentId) ?? incidents[0]
  const isServicesRoute = activeRoute === "Services"
  const isIncidentsRoute = activeRoute === "Incidents"
  const isOverviewRoute = activeRoute === "Overview"
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

    if (isIncidentsRoute) {
      const incident = selectedIncident as ReliabilityIncident | undefined
      return {
        service: incident?.service ?? "no incident",
        environment: "service reliability",
        releaseId: incident?.relatedRelease?.id ?? "no related release",
        version: "not reported",
        result: incident ? `${incident.severity} ${incident.status}` : "no active incident",
        imageDigest: "not reported",
      }
    }

    if (isOverviewRoute) {
      const overview = overviewQuery.data as ReliabilityOverview | undefined
      return {
        service: `${overview?.summary.totalServices ?? 0} services`,
        environment: "reliability fleet",
        releaseId: "fleet overview",
        version: "not reported",
        result: `${overview?.summary.activeIncidents ?? 0} active incidents`,
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
  }, [isIncidentsRoute, isOverviewRoute, isServicesRoute, overviewQuery.data, selected, selectedIncident, selectedService, selectedSummary])

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
    : isIncidentsRoute
      ? incidentsQuery.isLoading
      : isOverviewRoute
        ? overviewQuery.isLoading
      : releasesQuery.isLoading || latestQuery.isLoading
  const hasError = isServicesRoute
    ? servicesQuery.isError
    : isIncidentsRoute
      ? incidentsQuery.isError
      : isOverviewRoute
        ? overviewQuery.isError
      : releasesQuery.isError || latestQuery.isError

  function refreshAll() {
    void releasesQuery.refetch()
    void latestQuery.refetch()
    void servicesQuery.refetch()
    void incidentsQuery.refetch()
    void overviewQuery.refetch()
    void environmentEvidenceQuery.refetch()
  }

  return (
    <LayoutShell
      hasError={hasError}
      latest={latestQuery.data}
      generatedAt={isServicesRoute ? servicesQuery.data?.generatedAt : isIncidentsRoute ? incidentsQuery.data?.generatedAt : isOverviewRoute ? overviewQuery.data?.generatedAt : releasesQuery.data?.generatedAt}
      activeRoute={activeRoute}
      onRouteChange={setActiveRoute}
      releaseContext={releaseContext}
      onRefresh={refreshAll}
    >
      {isLoading ? (
        <PortalState kind="loading" />
      ) : hasError ? (
        <PortalState kind="error" />
      ) : isOverviewRoute && overviewQuery.data ? (
        <ReliabilityOverviewPage
          overview={overviewQuery.data}
          onOpenService={(serviceName) => {
            setSelectedServiceName(serviceName)
            setActiveRoute("Services")
          }}
          onOpenIncident={(incidentId) => {
            setSelectedIncidentId(incidentId)
            setActiveRoute("Incidents")
          }}
        />
      ) : isServicesRoute ? (
        <ServicesPage
          services={services}
          selected={selectedService}
          onSelect={setSelectedServiceName}
          onOpenRelease={(releaseId) => {
            setSelectedId(releaseId)
            setActiveRoute("Releases")
          }}
          onOpenIncident={(incidentId) => {
            setSelectedIncidentId(incidentId)
            setActiveRoute("Incidents")
          }}
        />
      ) : isIncidentsRoute ? (
        <IncidentsPage
          incidents={incidents}
          selected={selectedIncident}
          onSelect={setSelectedIncidentId}
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




