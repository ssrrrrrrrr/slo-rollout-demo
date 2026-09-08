# S Sentinel 演示流程

## 主流程：无新 Release 的 Runtime 故障

本演示验证 S Sentinel 能从持续可靠性信号进入受控恢复，而不依赖新的 Release。

1. Runtime 发现工作负载 `UNHEALTHY`。
2. Reliability Controller reconcile Service，并创建 `ACTIVE` Incident。
3. LLM Diagnosis 解释 Incident 证据，并推荐已注册的 `restart-unhealthy-workload` Runbook。
4. 用户预览 Recovery Plan；预览只做 eligibility、preflight 与计划，不产生 Kubernetes 变更。
5. 用户对当前确定性 plan 进行人工审批。
6. 在 Policy、Approval、Recovery Gate、Action Gate 与 Preflight 全部通过后，执行 `RESTART_WORKLOAD`。
7. ControlledOperation 进入 `EXECUTING`，并在 Durable Operation Ledger 中记录状态。
8. Runtime 进入 `RECOVERING`；Verification 同时检查 Runtime 和 SLO。
9. 验证通过后操作为 `RECOVERED`，Incident 进入 `RESOLVED`。

```text
Runtime UNHEALTHY
  → Controller reconcile
  → Incident ACTIVE
  → LLM Diagnosis
  → restart-unhealthy-workload
  → Preview
  → Human Approval
  → RESTART_WORKLOAD
  → Operation EXECUTING
  → Runtime RECOVERING
  → Verification RECOVERED
  → Incident RESOLVED
```

默认部署下 Recovery Gate 关闭，因此审批不会直接产生 Kubernetes mutation。

## Release 关联场景

近期失败的 Release 可与 Incident 关联。若推荐为 `ROLLBACK_RELEASE`，它仍需经过既有 Policy、Approval、Gate、Runtime Action 与后置验证链；历史过期的失败 Release 只保留为关联信息，不单独构成当前风险。
