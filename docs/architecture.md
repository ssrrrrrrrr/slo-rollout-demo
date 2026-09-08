# S Sentinel 架构

S Sentinel 将服务可靠性信号、事件判断与受控恢复分层组织。Service 是平台入口；Release 保留为可靠性信号和 Runtime Action 的兼容来源，而不是唯一根对象。

```text
Service / SLO / Runtime / Release
              ↓
Reliability Controller → Incident Lifecycle
              ↓
LLM Diagnosis → Runbook Matcher → Recovery Planner
              ↓
Policy / Approval → ControlledOperation → Durable Ledger → Executor → Verification
```

## Observation

Service 配置定义运行时引用、SLO 引用和交付策略。SLO 服务通过 Prometheus 计算 Availability、Error Rate、P95 Latency、Error Budget 与 Burn Rate；Runtime 服务读取 Kubernetes/Argo Rollouts 状态；Release Evidence 提供近期发布及其结果。

这些信号是观测输入。单次 Release Analysis 不是 Service 长期 SLO 状态的替代品。

## Incident

Reliability Controller 定期和按需为每个 Service 进行 reconcile。Detector 关联 SLO、Runtime 与具备运行相关性的近期 Release；Lifecycle 根据稳定 fingerprint 创建、更新、恢复或解决 Incident。

Incident 与时间线持久化在 SQLite。相同 Service 的一次 lifecycle transaction 在进程内串行化，避免 Controller 与手工 reconcile 竞争创建重复 episode。

## Reasoning

LLM Diagnosis 可读取 Incident 证据，解释可能原因，并在已注册 Runbook 中推荐候选。Runbook Matcher 和 Recovery Planner 决定候选动作与不可变目标。

LLM 不拥有执行能力：不能调用 shell 或 kubectl，不能执行或审批操作，不能绕过 Policy，也不能自行触发恢复。LLM 不可用时使用确定性诊断/计划边界，系统仍保持安全。

## Control

真实恢复遵循固定顺序：

`Runbook → Policy → Approval → Preflight → Gate → ControlledOperation → Executor → Verification`

- Policy 与 Approval 约束动作是否可以继续；Recovery 默认关闭。
- ControlledOperation 是一次受控执行的规范对象，Operation ID 是规范执行身份。
- Durable Operation Ledger 将操作及状态持久化到 SQLite；Ledger 不可用时真实变更 fail closed。
- Executor Registry 仅选择已注册执行器：Kubernetes Recovery Executor 用于 RESTART_WORKLOAD/SCALE_WORKLOAD，Release Runtime Action Adapter 复用已有 PAUSE/RESUME/PROMOTE/ABORT/ROLLBACK 链。
- Verification 读取后置 Runtime 和 SLO 状态；成功退出码不等同于已恢复。

## 持久化与崩溃语义

- Incident Lifecycle 和 Operation Ledger 均使用 SQLite 持久化。
- RESTART_WORKLOAD 的 `restartAt`、SCALE_WORKLOAD 的 `targetReplicas` 在操作创建后不可变。
- watcher 崩溃后，处于 `EXECUTING` 的操作只允许 Inspect；不会自动再次 Execute。
- `FAILED` 与 `UNKNOWN` 操作不会自动重试。
- 该语义不宣称 exactly-once；它提供可追溯、保守的崩溃恢复边界。

## Release 兼容边界与 GitOps

保留的 Release 能力包括：Release correlation、最小 Release Evidence、Runtime Action、PAUSE/RESUME/PROMOTE/ABORT/ROLLBACK，以及 Kustomize + Argo CD GitOps 交付。Release Advisor/Evidence compatibility 仍为兼容能力。

已移除的旧链路包括 GitOps Proposal/Bundle/Handoff、GitOps Adapter、Real PR artifact workflow 与 Noop Execution plane；它们不是当前产品执行路径。

当前 GitOps 交付链只有：

`Config → Config Compiler → Kustomize → Git → Argo CD → Kubernetes / Argo Rollouts`

## 基础设施

- Kubernetes 与 Argo Rollouts：运行时状态和受控变更目标。
- Prometheus：SLO 指标来源。
- SQLite：Incident 与 Operation Ledger 的本地持久化。
- Ollama：可选 LLM 诊断后端。
- Kustomize 与 Argo CD：声明式配置交付。
