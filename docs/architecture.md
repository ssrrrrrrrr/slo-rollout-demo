# S Sentinel Architecture

S Sentinel is a Service-first reliability platform. Its platform core is
separate from, but connected to, the existing artifact-based Release Control
Plane.

## Platform Core

`Service` is the entry point. A Service configuration references its runtime,
SLO, and delivery strategy. `SLOService` evaluates configured Prometheus
objectives; `RuntimeService` reads the configured Argo Rollout; `IncidentService`
correlates those current signals with a fresh related release; and
`OverviewService` aggregates fleet state.

These Service, SLO, Runtime, Incident, Overview, and Remediation domains do not
depend on `latest.json` workflow files.

## Release Control Plane

Release Context, Evidence, Advisor, Policy, Approval, Execution, and GitOps
remain an established compatibility subsystem. It supplies release-scoped
evidence and recommendations; it is not replaced by the Service domain.

## Controlled Remediation

An Incident may project an existing, fresh release recommendation. The plan
reuses its policy and approval state. Execution is delegated only to the
existing Runtime Action pipeline and consumes its execution result and
post-action verification. No Incident-specific executor or direct kubectl
implementation exists.

## Safety Boundaries

Runtime actions are default-off and require policy, approval, matching
preflight, `S_SENTINEL_RUNTIME_EXECUTION_ENABLED`, an action allow gate,
`S_SENTINEL_RUNTIME_ACTION_APPROVED`, the action execute switch, and successful
post-state verification. GitOps writes are not added by the remediation layer.

## Infrastructure

Kubernetes and Argo Rollouts provide runtime state; Prometheus provides SLO
measurements; SQLite backs the existing Evidence Store runtime. All are external
to the Service domain and can be unavailable without making Service APIs panic.
