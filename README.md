# S Sentinel

S Sentinel 是一个面向 Kubernetes 服务的 SLO 驱动可靠性控制平台。

平台持续观察 Service 的 SLO、Runtime 与 Release 状态，自动发现并关联可靠性 Incident；通过 LLM 辅助诊断、Runbook、Policy、人工审批和受控执行，对部分明确且安全的问题形成恢复闭环。

## 核心能力

- Service-first：以 Service 聚合 SLO、Runtime、Release 与 Incident。
- SLO：Availability、Error Rate、P95 Latency、Error Budget 与 Burn Rate。
- Runtime：面向 Argo Rollouts 的 Kubernetes 运行状态快照。
- Reliability Controller：持续 reconcile，创建、更新和关闭 durable Incident。
- Incident：持久化生命周期、Timeline、Release correlation 与 LLM-assisted diagnosis。
- Recovery：注册 Runbook、Recovery Plan、Policy、人工审批、Verification。
- ControlledOperation：统一 Recovery 与 Release Runtime Action 的受控执行合同。
- Durable Operation Ledger：持久化不可变执行 intent、执行状态与崩溃后的 inspect-before-retry 语义。

## 架构

```text
Observe → Detect → Correlate → Diagnose → Plan → Govern → Execute → Verify

Service / SLO / Runtime / Release
  → Incident Lifecycle
  → Reliability Agent / Runbook
  → Policy / Approval
  → ControlledOperation / Durable Ledger
  → Executor / Verification
```

Release Control Plane、Evidence、GitOps 与既有 Runtime Action 保持为兼容子系统；Release 是 Service Reliability 的一个信号来源，而不是平台唯一入口。

## 安全边界

- Controller 只负责持续检测，不会自动审批或自动执行恢复。
- Agent 只提出诊断与已注册 Runbook 候选，不能执行、审批、绕过 Policy 或调用 shell/kubectl。
- Preview 永远只读；Approval 与 Execute 是两个独立步骤。
- Recovery 与 Release Runtime Action 默认关闭，真实 mutation 必须满足 Policy、Approval、Preflight 和全部 Gate。
- Operation Ledger 不可用时，真实执行 fail-closed；读取、诊断和预览能力保持可用。
- 崩溃后的 EXECUTING Operation 只检查外部状态，不会自动重试 mutation。

## 典型场景

`demo-app` Runtime 变为 UNHEALTHY 后，Controller 创建 durable Incident；Agent 给出 `RUNTIME_FAILURE` 和已注册的 `restart-unhealthy-workload` 候选。操作者预览、审批并在全部 Gate 满足时执行 `RESTART_WORKLOAD`，随后由 Runtime/SLO Verification 推进 Incident 至 RECOVERING 或 RESOLVED。

## 技术栈

Go Watcher、React/Vite Portal、Kubernetes/Argo Rollouts、Prometheus、SQLite、可选 Ollama，以及既有 Release/Evidence 脚本兼容层。

## 当前限制

- 推荐单 watcher 部署；暂无 leader election 或 distributed lock。
- 无自动恢复与自动重试。
- Runtime Recovery 当前主要面向 Argo Rollouts。
- SLO 依赖 Prometheus 指标可用性。
- Release compatibility 仍保留 artifact/script 组件。

## 文档入口

- [架构](docs/architecture.md)
- [演示流程](docs/demo.md)
- 本地验证：`cd watcher && go test ./...`、`cd web && pnpm run build`、`bash scripts/test-release-contracts.sh`
