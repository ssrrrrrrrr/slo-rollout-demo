import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { approveRecovery, executeRecovery, fetchRecovery, previewRecovery } from "@/api/recovery"
import { KeyValueRows } from "@/components/common/KeyValueRows"
import { Panel } from "@/components/common/Panel"

export function IncidentRecoveryPanel({ incidentId }: { incidentId: string }) {
  const client = useQueryClient()
  const query = useQuery({ queryKey: ["incident-recovery", incidentId], queryFn: () => fetchRecovery(incidentId) })
  const refresh = () => void client.invalidateQueries({ queryKey: ["incident-recovery", incidentId] })
  const preview = useMutation({ mutationFn: () => previewRecovery(incidentId), onSuccess: refresh })
  const approve = useMutation({ mutationFn: (id: string) => approveRecovery(incidentId, id), onSuccess: refresh })
  const execute = useMutation({ mutationFn: (id: string) => executeRecovery(incidentId, id), onSuccess: refresh })
  if (query.isLoading) return <Panel tone="muted" className="text-sm">Loading recovery plan…</Panel>
  if (query.isError || !query.data) return <Panel tone="muted" className="text-sm">Recovery data unavailable</Panel>
  const plan = query.data.recovery
  return <Panel>
    <p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-500">Recovery</p>
    <h4 className="mt-2 text-lg font-semibold text-slate-100">{plan.matchedRunbook?.metadata.name || "No safe recovery runbook"}</h4>
    <p className="mt-1 text-sm text-slate-400">{plan.reason || plan.diagnosis.reason || "Recovery is evaluated from current Service runtime state."}</p>
    <div className="mt-4"><KeyValueRows rows={[["Diagnosis", plan.diagnosis.category], ["Action", plan.action.type || "none"], ["Target", `${plan.target.namespace}/${plan.target.kind}/${plan.target.name}`], ["Risk", plan.risk || "not reported"], ["Policy", plan.policy.decision || "not reported"], ["Approval", plan.approval.required ? (plan.approval.approved ? "approved" : "required") : "not required"], ["Verification", plan.verification?.status || "PENDING"]]} /></div>
    {plan.preflight.blockingReasons.length ? <p className="mt-4 text-sm text-amber-200">{plan.preflight.blockingReasons.join("; ")}</p> : null}
    {plan.status !== "NOT_ACTIONABLE" ? <div className="mt-5 flex gap-3"><button type="button" onClick={() => preview.mutate()} className="rounded-lg border border-[#35517a] bg-[#14233a] px-3 py-2 text-sm font-semibold text-slate-100">Preview recovery</button>{plan.approval.required && !plan.approval.approved ? <button type="button" onClick={() => approve.mutate(plan.id)} disabled={approve.isPending} className="rounded-lg border border-amber-800 bg-amber-950/35 px-3 py-2 text-sm font-semibold text-amber-100">Approve Recovery</button> : null}{plan.approval.approved ? <span className="self-center text-sm font-semibold text-emerald-200">Approved</span> : null}{plan.preflight.eligible ? <button type="button" disabled={execute.isPending} onClick={() => execute.mutate(plan.id)} className="rounded-lg border border-rose-800 bg-rose-950/35 px-3 py-2 text-sm font-semibold text-rose-100 disabled:opacity-60">Execute {plan.action.type}</button> : null}</div> : null}
  </Panel>
}
