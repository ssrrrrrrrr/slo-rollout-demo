import { useQueries } from "@tanstack/react-query"
import { Activity, FileText, ShieldCheck } from "lucide-react"
import {
  fetchReleaseResource,
  formatResourceBody,
  isMarkdownContent,
  type ReleaseResourceKind,
} from "@/api/releaseResources"
import type { LatestReleaseResponse, ReleaseIndexItem } from "@/types/release"
import { SafetyPanel } from "./SafetyPanel"

type GroupItem = {
  label: string
  kind: ReleaseResourceKind
  description: string
}

const groupedResources: Record<string, { title: string; description: string; items: GroupItem[] }> = {
  "Runtime Actions": {
    title: "Runtime Actions",
    description: "Controlled runtime action evidence grouped by recommendation, request, preflight, execution, and safety boundary.",
    items: [
      { label: "Recommendation", kind: "runtime-action-recommendation", description: "Read-only runtime action recommendation." },
      { label: "Request", kind: "runtime-action-request", description: "Policy-bound runtime action request." },
      { label: "Preflight", kind: "runtime-action-preflight", description: "Runtime action eligibility and safety checks." },
      { label: "Execution Result", kind: "runtime-action-execution-result", description: "Runtime Action result, receipt, and verification artifact." },
      { label: "Policy Decision", kind: "policy-decision", description: "Policy decision used before runtime execution." },
      { label: "AI Decision", kind: "ai-decision", description: "Advisor decision that may recommend an action." },
    ],
  },
  Advisor: {
    title: "Advisor",
    description: "Read-only AI advisor output and release intelligence artifacts.",
    items: [
      { label: "Advisor Trace", kind: "advice", description: "Advisor markdown output and model context." },
      { label: "Intelligence", kind: "intelligence", description: "Structured release intelligence artifact." },
      { label: "AI Decision", kind: "ai-decision", description: "Machine-readable AI decision artifact." },
    ],
  },
  Docs: {
    title: "Release Docs",
    description: "Human-readable operational documents generated from release evidence.",
    items: [
      { label: "Timeline", kind: "timeline", description: "Release timeline and event sequence." },
      { label: "Runbook", kind: "runbook", description: "Operator runbook for this release." },
      { label: "RCA", kind: "rca", description: "Root-cause analysis document." },
      { label: "Context", kind: "context", description: "Raw release context used by downstream artifacts." },
    ],
  },
}

function iconFor(tab: string) {
  if (tab === "Runtime Actions") return <ShieldCheck className="h-4 w-4 text-[#5d8fd8]" />
  return <Activity className="h-4 w-4 text-[#5d8fd8]" />
}

function shortBody(contentType: string, body: string) {
  const formatted = formatResourceBody(contentType, body)
    .replace(/\r/g, "")
    .split("\n")
    .filter((line) => line.trim().length > 0)
    .slice(0, 8)
    .join("\n")

  return formatted.length > 520 ? `${formatted.slice(0, 520)}...` : formatted
}

export function GroupedReleaseResourcePanel({
  activeTab,
  selected,
  latest,
}: {
  activeTab: string
  selected: ReleaseIndexItem
  latest?: LatestReleaseResponse
}) {
  const group = groupedResources[activeTab]

  const queries = useQueries({
    queries: group.items.map((item) => ({
      queryKey: ["release-resource-group", selected.releaseId, item.kind],
      queryFn: () => fetchReleaseResource(selected.releaseId, item.kind),
      retry: false,
      staleTime: 30_000,
    })),
  })

  return (
    <div className="space-y-5">
      {activeTab === "Runtime Actions" || activeTab === "GitOps" ? (
        <SafetyPanel latest={latest} />
      ) : null}

      <div className="rounded-xl border border-[#1f2b3d] bg-[#0b121d] p-5">
        <div className="flex flex-col gap-3 border-b border-[#1a2535] pb-4 md:flex-row md:items-start md:justify-between">
          <div>
            <div className="flex items-center gap-2 font-semibold text-slate-100">
              {iconFor(activeTab)}
              {group.title}
            </div>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-400">
              {group.description}
            </p>
          </div>
          <span className="rounded-full border border-[#1f2b3d] bg-[#070b12] px-3 py-1 text-xs font-semibold text-slate-400">
            grouped resources
          </span>
        </div>

        <div className="mt-5 grid gap-4 xl:grid-cols-2">
          {group.items.map((item, index) => {
            const query = queries[index]
            const isAvailable = Boolean(query.data)
            const isMissing = query.isError

            return (
              <div
                key={item.kind}
                className="rounded-xl border border-[#1f2b3d] bg-[#0f1724] p-4"
              >
                <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                  <div>
                    <div className="flex items-center gap-2 font-semibold text-slate-100">
                      <FileText className="h-4 w-4 text-slate-500" />
                      {item.label}
                    </div>
                    <p className="mt-1 text-xs leading-5 text-slate-500">{item.description}</p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <span className="rounded-full border border-[#26364d] bg-[#070b12] px-2.5 py-1 text-xs font-semibold text-slate-400">
                      {item.kind}
                    </span>
                    <span
                      className={`rounded-full border px-2.5 py-1 text-xs font-semibold ${
                        query.isLoading
                          ? "border-slate-700 bg-slate-950/40 text-slate-400"
                          : isAvailable
                            ? "border-emerald-900/50 bg-emerald-950/20 text-emerald-300"
                            : "border-amber-900/50 bg-amber-950/20 text-amber-300"
                      }`}
                    >
                      {query.isLoading ? "loading" : isAvailable ? "available" : "missing"}
                    </span>
                  </div>
                </div>

                <div className="mt-4 rounded-lg border border-[#1f2b3d] bg-[#070b12] p-3">
                  {query.isLoading ? (
                    <p className="text-sm text-slate-500">Loading resource...</p>
                  ) : isMissing ? (
                    <p className="text-sm leading-6 text-slate-500">
                      This release does not have this artifact yet, or the API returned a missing resource.
                    </p>
                  ) : query.data ? (
                    <pre className="max-h-56 overflow-auto whitespace-pre-wrap text-xs leading-5 text-slate-300">
                      {isMarkdownContent(query.data.contentType)
                        ? shortBody(query.data.contentType, query.data.body)
                        : shortBody(query.data.contentType, query.data.body)}
                    </pre>
                  ) : (
                    <p className="text-sm text-slate-500">No content.</p>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
