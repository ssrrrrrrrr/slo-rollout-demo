# S Sentinel Demo Runbook

This 5–10 minute walkthrough uses `demo-app`. Keep Runtime Action gates off for
the normal demo.

## 1. Healthy Service

Open **Overview**, then **Services** and `demo-app`. Show the configured runtime
reference, SLO status, error budget, burn rates, and Runtime snapshot.

## 2. SLO Degradation

Use the existing demo-app release inputs `FAULT_RATE` or `LATENCY_MS` to create
errors or latency. Generate traffic as required by the existing demo deployment.
Show the changed error rate, availability, P95 latency, error budget, burn rate,
and the resulting `AT_RISK` or `BREACHED` SLO status.

## 3. Release Correlation

Open **Incidents**, select the active incident, and follow **Open Release
Control Room**. Show the temporal related release, Evidence, Policy, and the
existing Advisor recommendation.

## 4. Controlled Remediation

On the Incident detail page, open **Controlled Remediation** and use Preview.
With default settings the plan is blocked; this is the expected safe demo.

Never enable Runtime Action gates merely to complete this walkthrough. A real
PAUSE, ABORT, or ROLLBACK demonstration requires an approved matching preflight,
policy allowance, and all of these explicit environment variables:

```text
S_SENTINEL_RUNTIME_EXECUTION_ENABLED=true
S_SENTINEL_RUNTIME_ACTION_APPROVED=true
S_SENTINEL_ALLOW_RUNTIME_<ACTION>=true
S_SENTINEL_RUNTIME_<ACTION>_EXECUTE=true
```

After an opt-in real action, inspect the existing Runtime Action result,
post-action rollout observation, Runtime snapshot, and SLO status. A 30-day SLO
may remain `BREACHED` while Runtime recovery is reported as `RECOVERING`.
