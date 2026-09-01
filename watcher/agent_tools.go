package main

// ReliabilityAgentTools is intentionally read-only. It exposes existing domain
// projections only and has no executor, approval, shell, or Kubernetes client.
type ReliabilityAgentTools struct {
	services  *ServiceService
	slo       *SLOService
	runtime   *RuntimeService
	incidents *IncidentService
	recovery  *RecoveryService
}

func (t ReliabilityAgentTools) ListApplicableRunbooks() ([]Runbook, error) { return t.recovery.Load() }
