# S Sentinel Architecture

S Sentinel is an **SLO-driven Reliability Control Platform**. Its core loop is:

```text
Observe -> Detect -> Correlate -> Diagnose -> Plan -> Govern -> Execute -> Verify
```

The implementation deliberately keeps four planes. They are boundaries for
responsibility, not separate services.

## Observation Plane

`Service` is the entry point. `ServiceService` loads configuration and exposes
runtime, reliability, and delivery references. `SLOService` evaluates the
referenced Prometheus SLO configuration; `RuntimeService` obtains an Argo
Rollout and Pod snapshot; Service releases are read through the existing
Evidence/Release compatibility path.

Provider failure is represented as `UNKNOWN` rather than invented healthy data.
This makes the Portal usable in local provider-unavailable mode without
claiming an active Runtime watcher.

## Incident Plane

`ReliabilityController` periodically reconciles configured Services.
`ReliabilityIncidentDetector` correlates current SLO, Runtime, and relevant
release observations. `IncidentLifecycleService` serializes each Service
reconcile, maps an active fingerprint to one active episode, and persists an
Incident plus Timeline through `IncidentRepository`.

`IncidentFingerprint` identifies an active episode; `IncidentID` identifies a
durable incident record. Reconciliation updates the current active episode. If
the same condition recurs after resolution, a new Incident is created. The
durable lifecycle states are `ACTIVE`, `MITIGATING`, `RECOVERING`, and
`RESOLVED`.

The Controller performs continuous detection only. It does not invoke the
Agent, approve, or execute recovery automatically.

## Reasoning Plane

`ReliabilityAgentService` reads an Incident context and produces a diagnosis
plus candidate Runbooks. The provider may use Ollama, but invalid/unavailable
output falls back to a deterministic provider. Candidate IDs are filtered to
the registered Runbooks loaded by `RecoveryService`.

The Agent does not execute actions, approve operations, bypass Policy, run a
shell or `kubectl`, issue arbitrary commands, or enable automatic remediation.

```text
Agent proposes. Control Plane validates. Policy governs.
Human approves. Executor executes. Verification confirms.
```

`RunbookMatcher` and the Recovery planner convert an applicable registered
Runbook into a Recovery Plan. They do not mutate Runtime state.

## Control Plane

`ControlledOperation` is the canonical controlled-execution contract used by
both Recovery and Release-derived Remediation. It records source, subject,
action, target, policy, approval checks, preflight, immutable execution intent,
execution summary, verification, and an idempotency key.

The durable sequence is:

```text
load/create ledger record
  -> materialize immutable intent
  -> refresh and validate policy, approval, and preflight
  -> persist READY
  -> persist EXECUTING
  -> Executor Registry dispatch
  -> persist executor result
  -> verification
```

The SQLite `OperationRepository` is authoritative for execution identity and
lifecycle state. Process-local locks only serialize same-operation access; they
do not replace durable identity.

`RESTART_WORKLOAD` persists `restartAt`; `SCALE_WORKLOAD` persists
`targetReplicas`; release actions persist their release/action/target identity.
These values are not regenerated after restart.

`OperationLifecycleService.ReconcileInFlight` is inspect-before-retry crash
handling. For an interrupted `EXECUTING` operation it inspects external state:
`APPLIED` continues the lifecycle, while `NOT_APPLIED` or `UNKNOWN` becomes
`UNKNOWN`. It never executes an operation again automatically. This is durable
and safe recovery semantics, not an exactly-once distributed execution claim.

The `OperationExecutorRegistry` selects either the Kubernetes recovery adapter
for bounded Runbook actions or the existing Release Runtime Action adapter for
PAUSE, RESUME, PROMOTE, ABORT, and ROLLBACK. No incident-specific `kubectl`
implementation or second executor is introduced.

## Infrastructure and Compatibility

| Dependency | Role |
|---|---|
| Kubernetes / Argo Rollouts | Runtime observation and bounded recovery adapter |
| Prometheus | SLO, Error Budget, and Burn Rate evaluation |
| SQLite | Durable incident, timeline, approval, and operation state |
| Ollama | Optional Reliability Agent provider |
| Release artifacts and scripts | Existing Release Control Plane compatibility pipeline |

The Release Control Plane remains responsible for Release Context, Evidence,
Advisor, Policy, Approval, Runtime Actions, and GitOps-related workflows. It
supplies a Release signal and compatible action artifacts to the Service
Reliability platform; it is not removed or rewritten.

## Safety Boundaries

All real mutations are default-off. A Recovery Execute validates the applicable
policy, durable human approval, preflight, `S_SENTINEL_RECOVERY_ENABLED`, and
the action-specific gate before it reaches an executor. Release Runtime Actions
also retain their established global, action-allow, approval, and execute gates.
Preview never reaches an executor, and approval alone never executes an action.

If the durable operation repository is unavailable, real Recovery and
Remediation execution fail closed. Read-only Service, SLO, Runtime, Incident,
Overview, Agent, and Preview paths remain available where their providers are.
