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
    id: "Releases",
    title: "Releases",
    description: "rollout history",
    group: "Platform",
    eyebrow: "Releases",
    pageTitle: "Release History & Detail",
    pageDescription: "查看发布历史、选中发布详情、资源摘要、时间线、Runbook、RCA 和原始审计内容。",
  },
  {
    id: "Incidents",
    title: "Incidents",
    description: "service reliability events",
    group: "Platform",
    eyebrow: "Incidents",
    pageTitle: "Reliability Incidents",
    pageDescription: "关联 SLO、Runtime 和已有 Release Evidence 的实时可靠性事件。",
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
    pageDescription: "解释本次发布为什么允许、阻断、需要人工审批或保持只读边界。",
  },
  {
    id: "Supply Chain",
    title: "Supply Chain",
    description: "镜像可信度与签名发布门禁",
    group: "Workspaces",
    eyebrow: "Supply Chain",
    pageTitle: "Signed Gate & Image Trust",
    pageDescription: "检查镜像摘要、GitOps 发布标签、Signed Release Gate、SupplyChainDecision 和供应链阻断原因。",
  },
  {
    id: "Agent Trace",
    title: "Advisor Trace",
    description: "Advisor 可观测链路",
    group: "Workspaces",
    eyebrow: "Advisor Trace",
    pageTitle: "Advisor Runtime Observability",
    pageDescription: "展示只读 Advisor 的 AgentRun、PolicyTrace、ToolCallTrace、EvidenceTrace 和 Guardrails。",
  },
  {
    id: "Approval",
    title: "Approval",
    description: "人工审批与执行申请",
    group: "Workspaces",
    eyebrow: "Approval",
    pageTitle: "Approval Boundary & Execution Request",
    pageDescription: "展示执行申请、人工审批状态、策略边界和只读执行约束，不提供直接执行入口。",
  },
  {
    id: "Environment",
    title: "Environment",
    description: "环境上下文与多环境证据",
    group: "Workspaces",
    eyebrow: "Environment",
    pageTitle: "Environment & Packaging Context",
    pageDescription: "展示当前发布的环境、集群、namespace、GitOps overlay、policy profile 和环境配置快照。",
  },
] as const

export type PortalRoute = (typeof portalRoutes)[number]["id"]
export type PortalRouteMeta = (typeof portalRoutes)[number]

export const defaultPortalRoute: PortalRoute = "Overview"

export const platformRoutes = portalRoutes.filter((route) => route.group === "Platform")
export const workspaceRoutes = portalRoutes.filter((route) => route.group === "Workspaces")

export function getPortalRouteMeta(routeId: PortalRoute): PortalRouteMeta {
  return portalRoutes.find((route) => route.id === routeId) ?? portalRoutes[0]
}
