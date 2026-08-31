import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { executeRemediation, fetchRemediation, fetchRemediationVerification, previewRemediation } from "@/api/remediation"
import { KeyValueRows } from "@/components/common/KeyValueRows"
import { Panel } from "@/components/common/Panel"
import type { RemediationPlan, RemediationVerificationStatus } from "@/types/remediation"

const verificationClass: Record<RemediationVerificationStatus, string> = {
  PENDING: "border-slate-700 bg-slate-900 text-slate-300",
  RECOVERING: "border-amber-900/45 bg-amber-950/20 text-amber-200",
  RECOVERED: "border-emerald-900/45 bg-emerald-950/20 text-emerald-200",
  FAILED: "border-rose-900/45 bg-rose-950/20 text-rose-200",
  UNKNOWN: "border-slate-700 bg-slate-900 text-slate-300",
}

function detailRows(plan: RemediationPlan): [string, string][] {
  return [
    ["Recommendation", `${plan.recommendation.action}${plan.recommendation.source ? ` (${plan.recommendation.source})` : ""}`],
    ["Target", plan.target.releaseId || "not available"],
    ["Policy", plan.policy.decision || "not reported"],
    ["Approval", plan.approval.required ? (plan.approval.approved ? "approved" : "required") : "not required"],
    ["Execution", plan.execution?.status || "not executed"],
    ["Runtime action result", plan.execution?.runtimeActionExecutionResultId || "not recorded"],
  ]
}

export function IncidentRemediationPanel({ incidentId }: { incidentId: string }) {
  const queryClient = useQueryClient()
  const remediationQuery = useQuery({ queryKey: ["incident-remediation", incidentId], queryFn: () => fetchRemediation(incidentId) })
  const verificationQuery = useQuery({ queryKey: ["incident-remediation-verification", incidentId], queryFn: () => fetchRemediationVerification(incidentId) })
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["incident-remediation", incidentId] })
    void queryClient.invalidateQueries({ queryKey: ["incident-remediation-verification", incidentId] })
  }
  const preview = useMutation({ mutationFn: () => previewRemediation(incidentId), onSuccess: refresh })
  const execute = useMutation({ mutationFn: (action: string) => executeRemediation(incidentId, action), onSuccess: refresh })

  if (remediationQuery.isLoading) return <Panel tone="muted" className="text-sm">Loading controlled remediation…</Panel>
  if (remediationQuery.isError || !remediationQuery.data) return <Panel tone="muted" className="text-sm">Remediation data unavailable</Panel>

  const plan = remediationQuery.data.remediation
  const verification = verificationQuery.data?.verification ?? plan.verification
  const action = plan.allowedActions[0]
  const mutationError = preview.error || execute.error

  return (
    <Panel>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">Controlled Remediation</p>
          <h4 className="mt-2 text-lg font-semibold text-slate-100">Existing Release Control action</h4>
          <p className="mt-1 text-sm text-slate-400">{plan.reason || plan.operation || "The action is projected from existing release evidence and remains gate controlled."}</p>
        </div>
        <span className="rounded-full border border-[#35517a] bg-[#14233a] px-3 py-1 font-mono text-xs font-semibold text-slate-100">{plan.status}</span>
      </div>

      <div className="mt-4"><KeyValueRows rows={detailRows(plan)} /></div>

      {plan.status === "NOT_ACTIONABLE" ? <p className="mt-4 text-sm text-slate-400">No remediation action is available for this incident.</p> : null}
      {plan.eligibility.blockingReasons.length > 0 ? (
        <div className="mt-4 rounded-lg border border-amber-900/45 bg-amber-950/20 p-3 text-sm text-amber-100">
          {plan.eligibility.blockingReasons.join("; ")}
        </div>
      ) : null}

      {verification ? (
        <div className="mt-4 flex flex-wrap items-center gap-3 rounded-lg border border-[#1f2b3d] bg-[#070b12] p-3">
          <span className={`rounded-full border px-2 py-1 font-mono text-xs font-semibold ${verificationClass[verification.status]}`}>{verification.status}</span>
          <span className="text-sm text-slate-400">{verification.reason || "Verification pending"}</span>
        </div>
      ) : null}

      {plan.status === "ACTIONABLE" ? (
        <div className="mt-5 flex flex-wrap gap-3">
          <button type="button" onClick={() => preview.mutate()} disabled={preview.isPending} className="rounded-lg border border-[#35517a] bg-[#14233a] px-3 py-2 text-sm font-semibold text-slate-100 disabled:cursor-not-allowed disabled:opacity-60">
            {preview.isPending ? "Previewing…" : "Preview remediation"}
          </button>
          <button type="button" onClick={() => execute.mutate(action)} disabled={!plan.eligibility.eligible || execute.isPending} className="rounded-lg border border-rose-800 bg-rose-950/35 px-3 py-2 text-sm font-semibold text-rose-100 disabled:cursor-not-allowed disabled:opacity-60">
            {execute.isPending ? "Executing…" : `Execute ${action}`}
          </button>
        </div>
      ) : null}
      {mutationError ? <p className="mt-3 text-sm text-rose-300">Remediation request failed: {mutationError.message}</p> : null}
    </Panel>
  )
}
