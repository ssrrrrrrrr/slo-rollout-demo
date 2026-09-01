package main

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type incidentListResponse struct {
	SchemaVersion string                `json:"schemaVersion"`
	GeneratedAt   string                `json:"generatedAt"`
	Count         int                   `json:"count"`
	Items         []ReliabilityIncident `json:"items"`
}

type incidentDetailResponse struct {
	SchemaVersion string               `json:"schemaVersion"`
	Incident      *ReliabilityIncident `json:"incident"`
}

type serviceIncidentListResponse struct {
	SchemaVersion string                `json:"schemaVersion"`
	GeneratedAt   string                `json:"generatedAt"`
	Service       string                `json:"service"`
	Count         int                   `json:"count"`
	Items         []ReliabilityIncident `json:"items"`
}

type serviceActiveIncidentResponse struct {
	SchemaVersion string               `json:"schemaVersion"`
	Service       string               `json:"service"`
	Incident      *ReliabilityIncident `json:"incident"`
}

func (api *portalAPI) handleIncidentList(w http.ResponseWriter, r *http.Request) {
	if !api.requireGET(w, r) {
		return
	}
	incidents, err := api.incidentService().List(r.Context(), r)
	if err != nil {
		api.writeIncidentError(w, err)
		return
	}
	writePortalJSON(w, http.StatusOK, incidentListResponse{SchemaVersion: "incident.list/v1alpha1", GeneratedAt: time.Now().Format(time.RFC3339), Count: len(incidents), Items: incidents})
}

func (api *portalAPI) handleIncidentDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/incidents/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "remediation" {
		resource := strings.Join(parts[1:], "/")
		if resource == "remediation" || resource == "remediation/preview" || resource == "remediation/execute" || resource == "remediation/verification" {
			api.handleIncidentRemediation(w, r, parts[0], resource)
			return
		}
	}
	if len(parts) >= 2 && parts[0] != "" && parts[1] == "recovery" {
		resource := strings.Join(parts[1:], "/")
		if resource == "recovery" || resource == "recovery/preview" || resource == "recovery/approve" || resource == "recovery/execute" || resource == "recovery/verification" {
			api.handleIncidentRecovery(w, r, parts[0], resource)
			return
		}
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "analysis" {
		api.handleIncidentAnalysis(w, r, parts[0])
		return
	}
	if !api.requireGET(w, r) {
		return
	}
	id := rest
	if id == "" || strings.Contains(id, "/") {
		api.writeIncidentError(w, &IncidentNotFoundError{ID: id})
		return
	}
	incident, err := api.incidentService().Get(r.Context(), r, id)
	if err != nil {
		api.writeIncidentError(w, err)
		return
	}
	writePortalJSON(w, http.StatusOK, incidentDetailResponse{SchemaVersion: "incident/v1alpha1", Incident: incident})
}

func (api *portalAPI) handleServiceIncidents(w http.ResponseWriter, r *http.Request, serviceName string) {
	incident, err := api.incidentService().ActiveForService(r.Context(), r, serviceName)
	if err != nil {
		api.writeIncidentError(w, err)
		return
	}
	items := []ReliabilityIncident{}
	if incident != nil {
		items = append(items, *incident)
	}
	writePortalJSON(w, http.StatusOK, serviceIncidentListResponse{SchemaVersion: "incident.serviceList/v1alpha1", GeneratedAt: time.Now().Format(time.RFC3339), Service: serviceName, Count: len(items), Items: items})
}

func (api *portalAPI) handleServiceActiveIncident(w http.ResponseWriter, r *http.Request, serviceName string) {
	incident, err := api.incidentService().ActiveForService(r.Context(), r, serviceName)
	if err != nil {
		api.writeIncidentError(w, err)
		return
	}
	writePortalJSON(w, http.StatusOK, serviceActiveIncidentResponse{SchemaVersion: "incident.active/v1alpha1", Service: serviceName, Incident: incident})
}

func (api *portalAPI) writeIncidentError(w http.ResponseWriter, err error) {
	var serviceNotFound *ServiceNotFoundError
	if errors.As(err, &serviceNotFound) {
		api.writeServiceError(w, err)
		return
	}
	var incidentNotFound *IncidentNotFoundError
	if errors.As(err, &incidentNotFound) {
		writePortalJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found", "id": incidentNotFound.ID})
		return
	}
	writePortalJSON(w, http.StatusInternalServerError, map[string]string{"error": "incident query failed", "detail": err.Error()})
}
