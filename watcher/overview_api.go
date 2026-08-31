package main

import "net/http"

func (api *portalAPI) handleReliabilityOverview(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}
	overview, err := api.overviewService().Get(r.Context(), r)
	if err != nil {
		writePortalJSON(w, http.StatusInternalServerError, map[string]string{"error": "reliability overview failed", "detail": err.Error()})
		return
	}
	writePortalJSON(w, http.StatusOK, overview)
}
