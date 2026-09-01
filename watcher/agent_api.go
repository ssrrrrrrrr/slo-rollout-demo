package main

import (
	"errors"
	"net/http"
)

func (api *portalAPI) handleIncidentAnalysis(w http.ResponseWriter, r *http.Request, id string) {
	svc := api.agentService()
	switch r.Method {
	case http.MethodGet:
		d := svc.Cached(r.Context(), r, id)
		if d == nil {
			writePortalJSON(w, 200, map[string]interface{}{"analysis": nil, "status": "NOT_ANALYZED"})
			return
		}
		writePortalJSON(w, 200, map[string]interface{}{"analysis": d})
	case http.MethodPost:
		d, e := svc.Analyze(r.Context(), r, id)
		if e != nil {
			var x *IncidentNotFoundError
			if errors.As(e, &x) {
				api.writeIncidentError(w, e)
				return
			}
			writePortalJSON(w, 500, map[string]string{"error": e.Error()})
			return
		}
		writePortalJSON(w, 200, map[string]interface{}{"analysis": d})
	default:
		api.writeRemediationMethodError(w)
	}
}
