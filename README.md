# S Sentinel

S Sentinel 是面向 Kubernetes 服务、以 SLO 为驱动的云原生可靠性控制平台。

它持续观察 Service 的 SLO、Runtime 与 Release 信号，发现并关联可靠性 Incident；再通过 LLM 辅助诊断、声明式 Runbook、Policy、人工审批与受控执行，为一部分明确问题形成安全、可验证的恢复闭环。

## 核心能力

- Service 视角的 SLO、Error Budget、Burn Rate 与 Kubernetes Runtime 可观测性。
- Reliability Controller 持续生成并收敛可持久化的 Incident 生命周期与时间线。
- LLM 辅助诊断：解释证据、给出诊断，并推荐已注册的 Runbook。
- 声明式 Runbook、Recovery Plan、Policy 与人工审批组成恢复治理链。
- ControlledOperation 与 SQLite Durable Operation Ledger 提供可审计的受控执行。
- 内置 RESTART_WORKLOAD、SCALE_WORKLOAD，以及 Release Runtime Action：PAUSE、RESUME、PROMOTE、ABORT、ROLLBACK。
- 执行后结合 Runtime 与 SLO 进行验证，而非以命令退出码作为恢复结论。

## 工作流程

`Observe → Detect → Correlate → Diagnose → Plan → Govern → Execute → Verify`

`Reliability Controller → SLO / Runtime / Release → Incident → LLM Diagnosis → Runbook / RecoveryPlan → Policy / Approval → ControlledOperation → Durable Operation Ledger → Executor → Verification`

## 架构

- Observation：Service、SLO、Runtime 与 Release 提供统一可靠性信号。
- Incident：Controller、Detector、Lifecycle、SQLite Repository 与 Timeline 管理事件生命周期。
- Reasoning：LLM Diagnosis、Runbook Matcher 与 Recovery Planner 将事件转为受限的恢复候选。
- Control：ControlledOperation、Policy、Approval、Durable Ledger、Executor Registry 与 Verification 执行并审计受控动作。

完整分层和持久化语义见 [架构说明](docs/architecture.md)。

## 安全边界

- Continuous Detection = YES；Automatic Remediation = NO。
- 真实变更必须经过：`Runbook → Policy → Approval → Preflight → Gate → ControlledOperation → Executor → Verification`。
- Recovery 默认关闭；Operation Ledger 不可用时，真实变更 fail closed。
- LLM 只能辅助诊断和推荐已注册 Runbook；不能执行 shell/kubectl、不能审批、不能绕过 Policy，也不能自动恢复。

## 典型场景

Runtime 故障且无新 Release 时，Controller 可创建 Incident；诊断推荐 `restart-unhealthy-workload` 后，用户先预览并审批，再执行受控的工作负载重启，最后由 Runtime/SLO 验证恢复。

近期失败的 Release 也可作为可靠性信号关联 Incident，并在满足既有治理条件时走受控的 ROLLBACK Runtime Action。

## 技术栈

Go watcher、React/Vite Web、Kubernetes/Argo Rollouts、Prometheus、SQLite、Ollama，以及 Kustomize + Argo CD GitOps 交付。

## 当前限制

- 推荐单 watcher 部署；尚无 leader election 或分布式锁。
- SQLite 采用单 watcher 模型。
- 不自动执行恢复，也不自动重试失败/未知操作。
- Runtime 恢复主要面向 Argo Rollouts 工作负载。
- SLO 数据依赖 Prometheus。
- Release 兼容性 artifact 与脚本仍保留。
- LLM 仅用于诊断与 Runbook 推荐。

## 文档

- [架构说明](docs/architecture.md)
- [演示流程](docs/demo.md)
- [Release / Evidence API 兼容说明](docs/release-portal-api.md)
- [Runtime Action 合约](docs/runtime-action-contract-audit.md)
