package main

import "net/http"

func (api *portalAPI) handleControllerStatus(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}
	if api.controller == nil {
		writePortalJSON(w, http.StatusOK, ReliabilityControllerStatus{Enabled: api.cfg.ReliabilityControllerEnabled, Running: false, Interval: api.cfg.ReliabilityReconcileInterval, Concurrency: api.cfg.ReliabilityReconcileConcurrency})
		return
	}
	writePortalJSON(w, http.StatusOK, api.controller.Status())
}
func (api *portalAPI) handleManualServiceReconcile(w http.ResponseWriter, r *http.Request, serviceName string) {
	if r.Method != http.MethodPost {
		api.writeRemediationMethodError(w)
		return
	}
	incidents := api.incidentService()
	if incidents.lifecycle == nil {
		writePortalJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "incident lifecycle persistence is unavailable"})
		return
	}
	incident, err := incidents.lifecycle.ReconcileService(r.Context(), r, serviceName)
	if err != nil {
		api.writeIncidentError(w, err)
		return
	}
	writePortalJSON(w, http.StatusOK, map[string]interface{}{"schemaVersion": "service.reconcile/v1alpha1", "service": serviceName, "incident": incident})
}
