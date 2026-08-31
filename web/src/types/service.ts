export type ServiceRuntimeRef = {
  namespace: string
  workload: {
    kind: string
    name: string
  }
}

export type ServiceReleaseSummary = {
  id: string
  status: string
  timestamp: string
}

export type ServiceSummary = {
  name: string
  displayName: string
  owner: string
  environments: string[]
  runtime: ServiceRuntimeRef
  sloRef: string
  strategyRef: string
  latestRelease: ServiceReleaseSummary | null
}

export type ServicesResponse = {
  schemaVersion: string
  generatedAt: string
  count: number
  items: ServiceSummary[]
}
