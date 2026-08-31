package main

import "net/http"

type serviceRuntimeResponse struct {
	SchemaVersion string          `json:"schemaVersion"`
	Runtime       RuntimeSnapshot `json:"runtime"`
}

func (api *portalAPI) handleServiceRuntime(w http.ResponseWriter, r *http.Request, serviceName string) {
	snapshot, err := api.runtimeService().Snapshot(r.Context(), serviceName)
	if err != nil {
		api.writeServiceError(w, err)
		return
	}

	writePortalJSON(w, http.StatusOK, serviceRuntimeResponse{
		SchemaVersion: "service.runtime/v1alpha1",
		Runtime:       snapshot,
	})
}
