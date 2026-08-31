package main

import (
	"encoding/json"
	"errors"
	"net/http"
)

func (api *portalAPI) handleIncidentRemediation(w http.ResponseWriter, r *http.Request, incidentID, resource string) {
	svc := api.remediationService()
	switch resource {
	case "remediation":
		if !api.requireGET(w, r) {
			return
		}
		plan, err := svc.Plan(r.Context(), r, incidentID)
		api.writeRemediationResponse(w, err, plan)
	case "remediation/preview":
		if r.Method != http.MethodPost {
			api.writeRemediationMethodError(w)
			return
		}
		plan, err := svc.Preview(r.Context(), r, incidentID)
		api.writeRemediationResponse(w, err, plan)
	case "remediation/execute":
		if r.Method != http.MethodPost {
			api.writeRemediationMethodError(w)
			return
		}
		var request struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writePortalJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid remediation request"})
			return
		}
		plan, err := svc.Execute(r.Context(), r, incidentID, request.Action)
		api.writeRemediationResponse(w, err, plan)
	case "remediation/verification":
		if !api.requireGET(w, r) {
			return
		}
		verification, err := svc.Verification(r.Context(), r, incidentID)
		if err != nil {
			api.writeRemediationError(w, err)
			return
		}
		writePortalJSON(w, http.StatusOK, map[string]interface{}{"schemaVersion": "incident.remediation.verification/v1alpha1", "verification": verification})
	}
}

func (api *portalAPI) writeRemediationResponse(w http.ResponseWriter, err error, plan RemediationPlan) {
	if err != nil {
		api.writeRemediationError(w, err)
		return
	}
	writePortalJSON(w, http.StatusOK, map[string]interface{}{"schemaVersion": "incident.remediation/v1alpha1", "remediation": plan})
}

func (api *portalAPI) writeRemediationMethodError(w http.ResponseWriter) {
	writePortalJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed", "allowedMethod": "POST"})
}

func (api *portalAPI) writeRemediationError(w http.ResponseWriter, err error) {
	var incidentNotFound *IncidentNotFoundError
	if errors.As(err, &incidentNotFound) {
		api.writeIncidentError(w, err)
		return
	}
	var requestError *RemediationRequestError
	if errors.As(err, &requestError) {
		writePortalJSON(w, requestError.StatusCode, map[string]interface{}{"error": requestError.Message})
		return
	}
	writePortalJSON(w, http.StatusInternalServerError, map[string]string{"error": "remediation query failed", "detail": err.Error()})
}
