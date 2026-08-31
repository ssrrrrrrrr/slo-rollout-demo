package main

import (
	"net/http"
)

type serviceSLOResponse struct {
	SchemaVersion string           `json:"schemaVersion"`
	SLO           ServiceSLOStatus `json:"slo"`
}

func (api *portalAPI) handleServiceSLO(w http.ResponseWriter, r *http.Request, serviceName string) {
	status, err := api.sloService().Evaluate(r.Context(), serviceName)
	if err != nil {
		api.writeServiceError(w, err)
		return
	}

	writePortalJSON(w, http.StatusOK, serviceSLOResponse{
		SchemaVersion: "service.slo/v1alpha1",
		SLO:           status,
	})
}
