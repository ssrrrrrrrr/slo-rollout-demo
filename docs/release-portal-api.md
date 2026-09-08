# Release / Evidence API 兼容说明

Release 仍是 S Sentinel 的可靠性信号和 Runtime Action 兼容来源。Service、Incident 与受控恢复是当前产品主入口；本页记录保留的 Release/Evidence 查询接口边界。

## Release 查询

- `GET /api/releases`
- `GET /api/releases/latest`
- `GET /api/releases/{id}`

这些接口提供 Release 摘要和详情，供 Release 工作区、Service Release 关联及 Incident correlation 使用。

## Evidence 查询

- `GET /api/evidence/releases`
- `GET /api/evidence/releases/{id}`
- `GET /api/evidence/objects/{kind}/{name}`
- `GET /api/evidence/artifacts`
- `GET /api/evidence/search`
- `GET /api/evidence/verification-summary`
- `GET /api/evidence/graph`

兼容路径 `/api/evidence-store/*` 继续提供现有 Evidence Store 客户端所需的查询和刷新能力。

## Latest Release 兼容资源

`/api/releases/latest/` 下保留以下读取资源：

- `evidence`、`evidence-record`、`summary`
- `action-plan`、`intelligence`、`approval`、`failure-evidence`
- `rollout-runtime-inspect`
- `runtime-action-recommendation`、`runtime-action-request`、`runtime-action-preflight`、`runtime-action-execution-result`
- `advice`、`memory`、`timeline`、`runbook`、`rca`

它们用于 Release compatibility 与 Evidence 展示。当前受控恢复不通过旧的通用 execution preview/result 资源执行。

## Runtime Action

PAUSE、RESUME、PROMOTE、ABORT、ROLLBACK 保留在既有 Release Runtime Action Pipeline 中。Incident/Recovery 需要 Release 动作时，经受控执行适配层复用该 Pipeline、既有 Gate、Execution Result 与 post-state verification；不会复制 kubectl 命令或另建执行器。

## 交付边界

当前声明式 GitOps 交付为：

`Config → Config Compiler → Kustomize → Git → Argo CD → Kubernetes / Argo Rollouts`

旧的 GitOps Proposal/Bundle/Handoff、GitOps Adapter 与 Real PR artifact workflow 已移除，不属于当前 Release API 或执行链。
