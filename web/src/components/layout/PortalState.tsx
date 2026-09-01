import { Panel, type PanelTone } from "@/components/common/Panel"

type PortalStateKind = "loading" | "error" | "empty"

const portalStateCopy: Record<PortalStateKind, { tone: PanelTone; message: string }> = {
  loading: {
    tone: "muted",
    message: "正在加载 Release Portal API...",
  },
  error: {
    tone: "danger",
    message: "Release Portal API 暂不可用。请检查 port-forward、后端服务状态，或 Vite proxy 是否指向本地后端地址。",
  },
  empty: {
    tone: "muted",
    message: "当前没有可展示的发布记录。",
  },
}

export function PortalState({ kind }: { kind: PortalStateKind }) {
  const state = portalStateCopy[kind]

  return (
    <Panel tone={state.tone} padding="lg" className="text-sm">
      {state.message}
    </Panel>
  )
}
