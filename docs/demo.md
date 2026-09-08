# S Sentinel Demo Runbook

This is the representative v1.0 demo: recovery from a Runtime failure without
a related Release. It demonstrates the Service Reliability loop rather than a
Release-only workflow.

Keep all recovery and Runtime Action gates disabled for normal observation and
preview. Enabling a real mutation is an explicit, separate operator decision.

## Runtime failure without Release

1. Open **Overview**, then **Services**, and select `demo-app` while Runtime is HEALTHY.
2. Cause the demo Rollout to become UNHEALTHY without creating a related Release.
3. The **Reliability Controller** reconciles the Service.
4. A durable Incident episode and timeline are created; its state is **ACTIVE**.
5. Open the Incident and request **Reliability Agent** analysis.
6. The expected diagnosis is `RUNTIME_FAILURE`.
7. The Agent recommends the registered `restart-unhealthy-workload` Runbook.
8. Open the Recovery Plan and inspect action, target, policy, gates, and preflight.
9. Use **Preview Recovery**. This is read-only and must not mutate Kubernetes.
10. With policy and preflight satisfied, a human explicitly approves the current Plan.
11. Only in a separately authorized environment, select **Execute RESTART_WORKLOAD**.
12. A durable ControlledOperation is created and enters `EXECUTING`.
13. The Kubernetes recovery adapter applies the Rollout restart.
14. Runtime shows the workload rebuilding.
15. The Incident moves to **RECOVERING** while verification is pending.
16. Runtime returns to HEALTHY.
17. Verification reports **RECOVERED**.
18. The Incident reaches **RESOLVED**.
19. Inspect the Incident timeline for the complete history and the durable
    Operation state returned by the Recovery API.

The key point is that no related Release is required for this recovery path.

## Safety prerequisites for a real action

Do not enable these variables merely to complete a demonstration. A real action
also requires an applicable Runbook, Policy allow, valid human approval, and
preflight readiness:

```text
S_SENTINEL_RECOVERY_ENABLED=true
S_SENTINEL_ALLOW_RECOVERY_<ACTION>=true
```

Release-derived Runtime Actions retain their own established gates, including:

```text
S_SENTINEL_RUNTIME_EXECUTION_ENABLED=true
S_SENTINEL_RUNTIME_ACTION_APPROVED=true
S_SENTINEL_ALLOW_RUNTIME_<ACTION>=true
S_SENTINEL_RUNTIME_<ACTION>_EXECUTE=true
```

Approval and execute are deliberately separate steps. A command exit code does
not itself mean recovery: inspect Runtime and SLO verification and the durable
operation result.

## Short release-correlated rollback scenario

For a fresh failed Release, open the correlated Incident and follow **Open
Release Control Room**. The existing Release Control Plane supplies Evidence,
Policy, approval, and an eligible rollback recommendation. The same governed
operation path executes the compatible Release Runtime Action only after every
existing gate is satisfied. A stale failed Release is correlation context, not
an automatic current-risk trigger.
