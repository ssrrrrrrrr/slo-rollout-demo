# Runtime Action 合约

Release Runtime Action 是保留的兼容执行链，支持 `PAUSE`、`RESUME`、`PROMOTE`、`ABORT` 与 `ROLLBACK`。它既可由 Release 工作区使用，也可被受控恢复通过适配层复用。

## 执行边界

受控恢复不会重新实现 Runtime Action，也不会直接拼接或调用 kubectl。它在动作可执行前完成 Runbook、Policy、Approval、Preflight 与 Runtime Gate 校验，再委托现有 Runtime Action Pipeline。

Pipeline 的 Execution Result（动作、状态、开始/结束时间、原因、目标与后置状态）被投射为受控操作与恢复验证信息。退出码本身不代表恢复完成；仍以既有 post-state verification、RuntimeService 和 SLOService 结果为准。

## 安全约束

- 全局 runtime execution gate、动作审批、动作 allow gate 与 execute gate 均须通过。
- Preview 只产生资格与计划信息，绝不调用真实执行适配层。
- Gate、Policy 或 Approval 任一层不满足时，不调用 Runtime Action Pipeline。
- 执行器不可用时返回受识别的阻塞状态，不伪造成功。

## Release 相关性

失败 Release 只有在 freshness window 内才可作为当前 Incident 的主触发器。过期或缺失时间戳的失败 Release 仍可作为历史关联证据，但不能单独升级当前风险或触发活动 Incident。
