export const portalRoutes = [
  {
    id: "Overview",
    title: "Overview",
    description: "fleet reliability",
    group: "Platform",
    eyebrow: "Overview",
    pageTitle: "Reliability Overview",
    pageDescription: "以 Service Fleet 为中心识别当前可靠性风险与优先处理项。",
  },
  {
    id: "Services",
    title: "Services",
    description: "service catalog",
    group: "Platform",
    eyebrow: "Services",
    pageTitle: "Service Catalog",
    pageDescription: "以 Service 为入口查看运行时、可靠性、交付策略和关联发布。",
  },
  {
    id: "Incidents",
    title: "Incidents",
    description: "service reliability events",
    group: "Platform",
    eyebrow: "Incidents",
    pageTitle: "Reliability Incidents",
    pageDescription: "关联 SLO、Runtime 和已有 Release Evidence 的可靠性事件。",
  },
  {
    id: "Releases",
    title: "Releases",
    description: "rollout history",
    group: "Platform",
    eyebrow: "Releases",
    pageTitle: "Release History & Detail",
    pageDescription: "查看发布历史、证据、Runtime Action 与时间线。",
  },
  {
    id: "Evidence",
    title: "Evidence",
    description: "EvidenceStore 检索与对象详情",
    group: "Platform",
    eyebrow: "Evidence",
    pageTitle: "Evidence Objects & Release Graph",
    pageDescription: "查看当前发布关联的 Evidence Object、控制平面对象链路和 EvidenceStore 检索结果。",
  },
  {
    id: "Policy",
    title: "Policy",
    description: "策略裁决解释与安全边界",
    group: "Platform",
    eyebrow: "Policy",
    pageTitle: "Policy Decision Explanation",
    pageDescription: "解释本次发布为什么允许、阻断或需要人工审批。",
  },
] as const

export type PortalRoute = (typeof portalRoutes)[number]["id"]
export type PortalRouteMeta = (typeof portalRoutes)[number]

export const defaultPortalRoute: PortalRoute = "Overview"
export const platformRoutes = portalRoutes

export function getPortalRouteMeta(routeId: PortalRoute): PortalRouteMeta {
  return portalRoutes.find((route) => route.id === routeId) ?? portalRoutes[0]
}
