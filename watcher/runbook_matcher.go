package main

import "strings"

func (DeterministicRecoveryPlanner) Plan(incident ReliabilityIncident, _ Service, runbooks []Runbook) *Runbook {
	// A fresh failed release with an existing rollback recommendation wins over
	// generic runtime recovery. All other recovery is driven by current runtime.
	rollback := incident.RelatedRelease != nil && incident.Recommendation != nil && strings.EqualFold(incident.Recommendation.Action, "ROLLBACK")
	for i := range runbooks {
		if rollback && runbooks[i].Spec.Action.Type == RecoveryRollbackRelease {
			return &runbooks[i]
		}
	}
	if incident.Runtime.Status != RuntimeStatusUnhealthy {
		return nil
	}
	for i := range runbooks {
		r := &runbooks[i]
		if r.Spec.Action.Type == RecoveryRestartWorkload && !r.Spec.Match.RequireRelease && matchesRuntime(r.Spec.Match.RuntimeStatus, incident.Runtime.Status) {
			return r
		}
	}
	return nil
}
func matchesRuntime(values []RuntimeStatus, actual RuntimeStatus) bool {
	for _, value := range values {
		if value == actual {
			return true
		}
	}
	return false
}
