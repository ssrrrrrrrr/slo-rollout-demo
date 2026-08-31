package main

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type serviceListResponse struct {
	SchemaVersion string           `json:"schemaVersion"`
	GeneratedAt   string           `json:"generatedAt"`
	Count         int              `json:"count"`
	Items         []ServiceSummary `json:"items"`
}

type serviceDetailResponse struct {
	SchemaVersion string         `json:"schemaVersion"`
	GeneratedAt   string         `json:"generatedAt"`
	Service       ServiceSummary `json:"service"`
}

type serviceReleasesResponse struct {
	SchemaVersion string                  `json:"schemaVersion"`
	GeneratedAt   string                  `json:"generatedAt"`
	Service       string                  `json:"service"`
	Count         int                     `json:"count"`
	Items         []ServiceReleaseSummary `json:"items"`
}

func (api *portalAPI) handleServiceList(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}

	services, err := api.serviceService().List(r)
	if err != nil {
		api.writeServiceError(w, err)
		return
	}

	writePortalJSON(w, http.StatusOK, serviceListResponse{
		SchemaVersion: "service.list/v1alpha1",
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Count:         len(services),
		Items:         services,
	})
}

func (api *portalAPI) handleServiceDetail(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}

	name, releases, ok := serviceRoute(r.URL.Path)
	if !ok {
		writePortalJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}

	serviceSvc := api.serviceService()
	if releases {
		items, err := serviceSvc.Releases(r, name)
		if err != nil {
			api.writeServiceError(w, err)
			return
		}
		writePortalJSON(w, http.StatusOK, serviceReleasesResponse{
			SchemaVersion: "service.releaseList/v1alpha1",
			GeneratedAt:   time.Now().Format(time.RFC3339),
			Service:       name,
			Count:         len(items),
			Items:         items,
		})
		return
	}

	service, err := serviceSvc.Get(r, name)
	if err != nil {
		api.writeServiceError(w, err)
		return
	}

	writePortalJSON(w, http.StatusOK, serviceDetailResponse{
		SchemaVersion: "service/v1alpha1",
		GeneratedAt:   time.Now().Format(time.RFC3339),
		Service:       service,
	})
}

func (api *portalAPI) writeServiceError(w http.ResponseWriter, err error) {
	var notFound *ServiceNotFoundError
	if errors.As(err, &notFound) {
		writePortalJSON(w, http.StatusNotFound, map[string]string{
			"error": "service not found",
			"name":  notFound.Name,
		})
		return
	}

	writePortalJSON(w, http.StatusInternalServerError, map[string]string{
		"error":  "service query failed",
		"detail": err.Error(),
	})
}

func serviceRoute(path string) (name string, releases bool, ok bool) {
	rest := strings.Trim(strings.TrimPrefix(path, "/api/v1/services/"), "/")
	if rest == "" {
		return "", false, false
	}

	parts := strings.Split(rest, "/")
	if len(parts) == 1 && strings.TrimSpace(parts[0]) != "" {
		return parts[0], false, true
	}
	if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && parts[1] == "releases" {
		return parts[0], true, true
	}

	return "", false, false
}
