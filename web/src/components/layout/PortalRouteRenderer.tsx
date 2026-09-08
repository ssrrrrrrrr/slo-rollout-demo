import type { UseQueryResult } from "@tanstack/react-query"
import type { ReleaseResourceContent } from "@/api/releaseResources"
import { getPortalRouteByReleaseTab } from "@/config/releaseTabs"
import { PortalInformationArchitecture } from "@/components/layout/PortalInformationArchitecture"
import { RoutePageHeader, type RouteHeaderBadgeTone } from "@/components/layout/RoutePageHeader"
import { StageBanner } from "@/components/layout/StageBanner"
import {
  getPortalRouteMeta,
  type PortalRoute,
} from "@/components/layout/portalRoutes"
import { ControlPlaneGraph } from "@/components/release/ControlPlaneGraph"
import { ControlPlaneObjectCards } from "@/components/release/ControlPlaneObjectCards"
import { EvidenceStorePanel } from "@/components/release/EvidenceStorePanel"
import { PolicyExplanationPanel } from "@/components/release/PolicyExplanationPanel"
import { ReleaseDetailWorkspace } from "@/components/release/ReleaseDetailWorkspace"
import { ReleaseMetricGrid } from "@/components/release/ReleaseMetricGrid"
import type { LatestReleaseResponse, ReleaseIndexItem } from "@/types/release"

function resultTone(value: string): RouteHeaderBadgeTone {
  const normalized = value.toLowerCase()

  if (normalized.includes("pass") || normalized.includes("allow") || normalized.includes("success")) {
    return "success"
  }

  if (normalized.includes("fail") || normalized.includes("deny") || normalized.includes("block")) {
    return "danger"
  }

  if (normalized.includes("pending") || normalized.includes("required") || normalized.includes("warn")) {
    return "warning"
  }

  return "neutral"
}

function boolTone(value: boolean): RouteHeaderBadgeTone {
  return value ? "warning" : "success"
}

function RouteHeader({
  activeRoute,
  selected,
  releaseCount,
}: {
  activeRoute: PortalRoute
  selected: ReleaseIndexItem
  releaseCount: number
}) {
  const summary = selected.summary
  const routeMeta = getPortalRouteMeta(activeRoute)

  return (
    <RoutePageHeader
      eyebrow={routeMeta.eyebrow}
      title={routeMeta.pageTitle}
      description={routeMeta.pageDescription}
      badges={[
        { label: "release", value: selected.releaseId, tone: "info" },
        { label: "result", value: summary.releaseResult, tone: resultTone(summary.releaseResult) },
        { label: "approval", value: String(summary.requiresHumanApproval), tone: boolTone(Boolean(summary.requiresHumanApproval)) },
        { label: "records", value: String(releaseCount), tone: "neutral" },
      ]}
    />
  )
}

export function PortalRouteRenderer({
  activeRoute,
  releases,
  selected,
  totalCount,
  onSelect,
  onRefresh,
  activeTab,
  onTabChange,
  onRouteChange,
  latest,
  resourceKind,
  resourceQuery,
  evidenceQuery,
  releaseCount,
}: {
  activeRoute: PortalRoute
  releases: ReleaseIndexItem[]
  selected: ReleaseIndexItem
  totalCount: number
  onSelect: (releaseId: string) => void
  onRefresh: () => void
  activeTab: string
  onTabChange: (tab: string) => void
  onRouteChange: (route: PortalRoute) => void
  latest?: LatestReleaseResponse
  resourceKind: string
  resourceQuery: UseQueryResult<ReleaseResourceContent, Error>
  evidenceQuery: UseQueryResult<ReleaseResourceContent, Error>
  releaseCount: number
}) {
  function handleRouteAwareTabChange(tab: string) {
    onTabChange(tab)

    const nextRoute = getPortalRouteByReleaseTab(tab)
    if (nextRoute) {
      onRouteChange(nextRoute)
    }
  }

  const pageHeader = (
    <RouteHeader
      activeRoute={activeRoute}
      selected={selected}
      releaseCount={releaseCount}
    />
  )

  switch (activeRoute) {
    case "Overview":
      return (
        <>
          {pageHeader}

          <StageBanner latest={latest} />

          <PortalInformationArchitecture
            latest={latest}
            releaseCount={releaseCount}
            activeTab={activeTab}
            onTabChange={handleRouteAwareTabChange}
          />

          <ReleaseMetricGrid selected={selected} />
        </>
      )

    case "Releases":
      return (
        <>
          {pageHeader}

          <ReleaseDetailWorkspace
            releases={releases}
            selected={selected}
            totalCount={totalCount}
            onSelect={onSelect}
            onRefresh={onRefresh}
            activeTab={activeTab}
            onTabChange={onTabChange}
            latest={latest}
            resourceKind={resourceKind}
            resourceQuery={resourceQuery}
          />
        </>
      )

    case "Evidence":
      return (
        <>
          {pageHeader}

          <ControlPlaneObjectCards
            selected={selected}
            evidenceQuery={evidenceQuery}
            onTabChange={handleRouteAwareTabChange}
          />

          <ControlPlaneGraph
            selected={selected}
            evidenceQuery={evidenceQuery}
            onTabChange={handleRouteAwareTabChange}
          />

          <EvidenceStorePanel
            selected={selected}
            onTabChange={handleRouteAwareTabChange}
          />
        </>
      )

    case "Policy":
      return (
        <>
          {pageHeader}

          <PolicyExplanationPanel
            selected={selected}
            evidenceQuery={evidenceQuery}
            onTabChange={handleRouteAwareTabChange}
          />
        </>
      )

    default:
      return null
  }
}






