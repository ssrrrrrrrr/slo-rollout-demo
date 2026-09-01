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
	name, resource, ok := serviceRoute(r.URL.Path)
	if !ok {
		writePortalJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}
	if resource == "reconcile" {
		api.handleManualServiceReconcile(w, r, name)
		return
	}
	if !api.requireGET(w, r) {
		return
	}

	serviceSvc := api.serviceService()
	if resource == "slo" {
		api.handleServiceSLO(w, r, name)
		return
	}
	if resource == "runtime" {
		api.handleServiceRuntime(w, r, name)
		return
	}
	if resource == "incidents" {
		api.handleServiceIncidents(w, r, name)
		return
	}
	if resource == "incidents/active" {
		api.handleServiceActiveIncident(w, r, name)
		return
	}

	if resource == "releases" {
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

func serviceRoute(path string) (name string, resource string, ok bool) {
	rest := strings.Trim(strings.TrimPrefix(path, "/api/v1/services/"), "/")
	if rest == "" {
		return "", "", false
	}

	parts := strings.Split(rest, "/")
	if len(parts) == 1 && strings.TrimSpace(parts[0]) != "" {
		return parts[0], "", true
	}
	if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && (parts[1] == "releases" || parts[1] == "slo" || parts[1] == "runtime" || parts[1] == "incidents" || parts[1] == "reconcile") {
		return parts[0], parts[1], true
	}
	if len(parts) == 3 && strings.TrimSpace(parts[0]) != "" && parts[1] == "incidents" && parts[2] == "active" {
		return parts[0], "incidents/active", true
	}

	return "", "", false
}
