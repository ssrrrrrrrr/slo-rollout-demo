import type { ReleaseResourceKind } from "@/api/releaseResources"
import type { PortalRoute } from "@/components/layout/portalRoutes"

export type ReleaseTabDefinition = {
  id: string
  resourceKind: ReleaseResourceKind
  targetRoute: PortalRoute
}

export const releaseTabs: ReleaseTabDefinition[] = [
  { id: "概览", resourceKind: "summary", targetRoute: "Releases" },
  { id: "Evidence", resourceKind: "evidence", targetRoute: "Releases" },
  { id: "Runtime Action", resourceKind: "execution-result", targetRoute: "Releases" },
  { id: "Timeline", resourceKind: "timeline", targetRoute: "Releases" },
]

export const releaseTabIds = releaseTabs.map((tab) => tab.id)

export function getReleaseTab(tabId: string): ReleaseTabDefinition | undefined {
  return releaseTabs.find((tab) => tab.id === tabId)
}

export function getReleaseResourceKindByTab(tabId: string): ReleaseResourceKind {
  return getReleaseTab(tabId)?.resourceKind ?? "summary"
}

export function getPortalRouteByReleaseTab(tabId: string): PortalRoute | null {
  return getReleaseTab(tabId)?.targetRoute ?? null
}
