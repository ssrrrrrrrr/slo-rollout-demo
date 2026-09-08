import { fetchText } from "@/api/client"
import { getReleaseResourceKindByTab } from "@/config/releaseTabs"

export type ReleaseResourceKind =
  | "summary"
  | "evidence"
  | "action-plan"
  | "preview"
  | "execution-result"
  | "intelligence"
  | "advice"
  | "context"
  | "ai-decision"
  | "policy-decision"
  | "runbook"
  | "rca"
  | "timeline"

export type ReleaseResourceContent = {
  releaseId: string
  kind: ReleaseResourceKind
  contentType: string
  body: string
}

export function getResourceKindByTab(tab: string): ReleaseResourceKind {
  return getReleaseResourceKindByTab(tab)
}

export function isMarkdownContent(contentType: string) {
  return contentType.toLowerCase().includes("markdown") || contentType.toLowerCase().includes("text/plain")
}

export function formatResourceBody(contentType: string, body: string) {
  if (isMarkdownContent(contentType)) {
    return body
  }

  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

export async function fetchReleaseResource(
  releaseId: string,
  kind: ReleaseResourceKind,
): Promise<ReleaseResourceContent> {
  const path = `/api/releases/${releaseId}/${kind}`
  const { contentType, body } = await fetchText(path)

  return {
    releaseId,
    kind,
    contentType,
    body,
  }
}
