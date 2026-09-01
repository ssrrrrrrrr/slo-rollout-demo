package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (api *portalAPI) handleRunbookList(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}
	items, err := api.recoveryService().Load()
	if err != nil {
		writePortalJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writePortalJSON(w, 200, map[string]interface{}{"schemaVersion": "runbook.list/v1alpha1", "generatedAt": time.Now().Format(time.RFC3339), "items": items})
}
func (api *portalAPI) handleRunbookDetail(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/runbooks/")
	items, err := api.recoveryService().Load()
	if err != nil {
		writePortalJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	for _, item := range items {
		if item.Metadata.Name == id {
			writePortalJSON(w, 200, map[string]interface{}{"schemaVersion": "runbook/v1alpha1", "runbook": item})
			return
		}
	}
	writePortalJSON(w, 404, map[string]string{"error": "runbook not found"})
}
func (api *portalAPI) handleIncidentRecovery(w http.ResponseWriter, r *http.Request, id, resource string) {
	svc := api.recoveryService()
	switch resource {
	case "recovery":
		if !api.requireGET(w, r) {
			return
		}
		p, e := svc.Plan(r.Context(), r, id)
		api.writeRecovery(w, e, p)
	case "recovery/preview":
		if r.Method != http.MethodPost {
			api.writeRemediationMethodError(w)
			return
		}
		p, e := svc.Preview(r.Context(), r, id)
		api.writeRecovery(w, e, p)
	case "recovery/approve":
		if r.Method != http.MethodPost {
			api.writeRemediationMethodError(w)
			return
		}
		planID, err := recoveryRequestPlanID(r)
		if err != nil {
			writePortalJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid recovery approval request"})
			return
		}
		p, e := svc.Approve(r.Context(), r, id, planID)
		api.writeRecovery(w, e, p)
	case "recovery/execute":
		if r.Method != http.MethodPost {
			api.writeRemediationMethodError(w)
			return
		}
		planID, err := recoveryRequestPlanID(r)
		if err != nil {
			writePortalJSON(w, 400, map[string]string{"error": "invalid recovery request"})
			return
		}
		p, e := svc.Execute(r.Context(), r, id, planID)
		api.writeRecovery(w, e, p)
	case "recovery/verification":
		if !api.requireGET(w, r) {
			return
		}
		v, e := svc.Verification(r.Context(), r, id)
		if e != nil {
			api.writeRecoveryError(w, e)
			return
		}
		writePortalJSON(w, 200, map[string]interface{}{"schemaVersion": "recovery.verification/v1alpha1", "verification": v})
	}
}
func recoveryRequestPlanID(r *http.Request) (string, error) {
	var body struct {
		PlanID string `json:"planId"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return "", err
	}
	if strings.TrimSpace(body.PlanID) == "" {
		return "", errors.New("planId is required")
	}
	return body.PlanID, nil
}
func (api *portalAPI) writeRecovery(w http.ResponseWriter, e error, p RecoveryPlan) {
	if e != nil {
		api.writeRecoveryError(w, e)
		return
	}
	writePortalJSON(w, 200, map[string]interface{}{"schemaVersion": "recovery/v1alpha1", "recovery": p})
}
func (api *portalAPI) writeRecoveryError(w http.ResponseWriter, e error) {
	var x *IncidentNotFoundError
	if errors.As(e, &x) {
		api.writeIncidentError(w, e)
		return
	}
	var q *RemediationRequestError
	if errors.As(e, &q) {
		writePortalJSON(w, q.StatusCode, map[string]string{"error": q.Message})
		return
	}
	writePortalJSON(w, 500, map[string]string{"error": e.Error()})
}
