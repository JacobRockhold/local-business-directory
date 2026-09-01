package main

import (
	"crypto/subtle"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (app *App) stats(w http.ResponseWriter, r *http.Request) {
	value, err := directoryStats(r.Context(), app.pool)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "stats_unavailable", "Directory statistics are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func searchRequestFromQuery(values url.Values) SearchRequest {
	limit, _ := strconv.Atoi(values.Get("limit"))
	return SearchRequest{
		Query: values.Get("query"), Categories: values["category"], City: values.Get("city"),
		Region: values.Get("region"), Country: values.Get("country"), Limit: limit,
	}
}

func (app *App) listBusinesses(w http.ResponseWriter, r *http.Request) {
	request := searchRequestFromQuery(r.URL.Query())
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 10_000_000 {
		writeError(w, http.StatusUnprocessableEntity, "offset_invalid", "Offset must be between 0 and 10,000,000.")
		return
	}
	businesses, total, err := searchBusinesses(r.Context(), app.pool, request, offset)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "directory_unavailable", "The directory could not be searched.")
		return
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 100
	}
	writeJSON(w, http.StatusOK, map[string]any{"businesses": businesses, "total": total, "limit": min(limit, 1000), "offset": offset})
}

func (app *App) business(w http.ResponseWriter, r *http.Request) {
	business, err := getBusiness(r.Context(), app.pool, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "business_not_found", "That business record was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "directory_unavailable", "The directory is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"business": business})
}

func (app *App) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := listJobs(r.Context(), app.pool, 100)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "jobs_unavailable", "Discovery jobs are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (app *App) job(w http.ResponseWriter, r *http.Request) {
	job, err := getJob(r.Context(), app.pool, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "job_not_found", "That discovery job was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "jobs_unavailable", "Discovery jobs are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (app *App) createDiscoveryJob(w http.ResponseWriter, r *http.Request) {
	if !requireMutation(w, r) {
		return
	}
	var request struct {
		Name   string   `json:"name"`
		Areas  []Area   `json:"areas"`
		Groups []string `json:"groups"`
	}
	if err := decodeJSON(w, r, &request, 1<<20); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "job_invalid", err.Error())
		return
	}
	if len(request.Areas) == 0 || len(request.Areas) > 50 {
		writeError(w, http.StatusUnprocessableEntity, "areas_invalid", "A bulk sweep needs between 1 and 50 areas.")
		return
	}
	for _, area := range request.Areas {
		if err := validateArea(area); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "area_invalid", err.Error())
			return
		}
	}
	groups, err := validateGroups(request.Groups)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "groups_invalid", err.Error())
		return
	}
	if len(request.Areas)*len(groups) > 350 {
		writeError(w, http.StatusUnprocessableEntity, "sweep_too_large", "A sweep may contain at most 350 area/category queries. Split this run into multiple jobs.")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "Bulk sweep " + time.Now().Format("2006-01-02 15:04")
	}
	if len(name) > 120 {
		writeError(w, http.StatusUnprocessableEntity, "name_invalid", "Job name must be 120 characters or fewer.")
		return
	}
	job := DiscoveryJob{
		ID: newID("job"), Name: name, Status: "queued", Areas: request.Areas, Groups: groups,
		TotalSteps: len(request.Areas) * len(groups), CreatedBy: r.Header.Get("X-FileForge-User-ID"), CreatedAt: time.Now().UTC(),
	}
	if err := createJob(r.Context(), app.pool, job); err != nil {
		writeError(w, http.StatusServiceUnavailable, "job_create_failed", "The bulk sweep could not be queued.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}

func (app *App) cancelDiscoveryJob(w http.ResponseWriter, r *http.Request) {
	if !requireMutation(w, r) {
		return
	}
	canceled, err := cancelJob(r.Context(), app.pool, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "job_cancel_failed", "The discovery job could not be canceled.")
		return
	}
	if !canceled {
		writeError(w, http.StatusConflict, "job_not_cancelable", "That job is already finished or does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"canceled": true})
}

func (app *App) retryDiscoveryJob(w http.ResponseWriter, r *http.Request) {
	if !requireMutation(w, r) {
		return
	}
	retried, err := retryJob(r.Context(), app.pool, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "job_retry_failed", "The failed requests could not be queued again.")
		return
	}
	if !retried {
		writeError(w, http.StatusConflict, "job_not_retryable", "Only a failed discovery job can be retried.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"retried": true})
}

func (app *App) getSettings(w http.ResponseWriter, r *http.Request) {
	value, err := settings(r.Context(), app.pool)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "Data-source settings are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": value})
}

func (app *App) putSettings(w http.ResponseWriter, r *http.Request) {
	if !requireMutation(w, r) {
		return
	}
	var value DirectorySettings
	if err := decodeJSON(w, r, &value, 32<<10); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "settings_invalid", err.Error())
		return
	}
	if err := validateEndpoint(value.OverpassEndpoint); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "endpoint_invalid", err.Error())
		return
	}
	minimum := 1
	if strings.EqualFold(mustURLHost(value.OverpassEndpoint), "overpass-api.de") {
		minimum = 5
	}
	if value.MinIntervalSecond < minimum || value.MinIntervalSecond > 60 {
		writeError(w, http.StatusUnprocessableEntity, "interval_invalid", fmt.Sprintf("Request interval must be between %d and 60 seconds for this endpoint.", minimum))
		return
	}
	if err := saveSettings(r.Context(), app.pool, value); err != nil {
		writeError(w, http.StatusServiceUnavailable, "settings_save_failed", "Data-source settings could not be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": value})
}

func mustURLHost(raw string) string {
	parsed, _ := url.Parse(raw)
	return parsed.Hostname()
}

func requireMutation(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-FileForge-User-ID")) == "" {
		writeError(w, http.StatusUnauthorized, "managed_route_required", "This action must be performed through FileForge.")
		return false
	}
	cookie, err := r.Cookie("hub_csrf")
	provided := r.Header.Get("X-CSRF-Token")
	if err != nil || cookie.Value == "" || provided == "" || len(cookie.Value) != len(provided) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(provided)) != 1 {
		writeError(w, http.StatusForbidden, "csrf_failed", "The security token is missing or expired. Refresh the page and try again.")
		return false
	}
	return true
}

func requireCaller(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-Hub-Caller-Plugin")) == "" {
		writeError(w, http.StatusUnauthorized, "hub_bridge_required", "This capability is available only through the approved FileForge bridge.")
		return false
	}
	return true
}

func (app *App) capabilitySearch(w http.ResponseWriter, r *http.Request) {
	if !requireCaller(w, r) {
		return
	}
	var request SearchRequest
	if err := decodeJSON(w, r, &request, 1<<20); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "search_invalid", err.Error())
		return
	}
	offset, err := decodeOffsetCursor(request.Cursor)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "cursor_invalid", err.Error())
		return
	}
	businesses, total, err := searchBusinesses(r.Context(), app.pool, request, offset)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "directory_unavailable", "The directory could not be searched.")
		return
	}
	result := map[string]any{"businesses": businesses, "total": total}
	if offset+len(businesses) < total {
		result["nextCursor"] = encodeOffsetCursor(offset + len(businesses))
	}
	writeJSON(w, http.StatusOK, result)
}

func (app *App) capabilityBackfill(w http.ResponseWriter, r *http.Request) {
	if !requireCaller(w, r) {
		return
	}
	var request BackfillRequest
	if err := decodeJSON(w, r, &request, 32<<10); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "backfill_invalid", err.Error())
		return
	}
	result, err := backfillBusinesses(r.Context(), app.pool, request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "backfill_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (app *App) capabilityGet(w http.ResponseWriter, r *http.Request) {
	if !requireCaller(w, r) {
		return
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(w, r, &request, 32<<10); err != nil || strings.TrimSpace(request.ID) == "" {
		writeError(w, http.StatusUnprocessableEntity, "get_invalid", "A business id is required.")
		return
	}
	business, err := getBusiness(r.Context(), app.pool, request.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "business_not_found", "That business record was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "directory_unavailable", "The directory is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"business": business})
}

func (app *App) exportCSV(w http.ResponseWriter, r *http.Request) {
	request := searchRequestFromQuery(r.URL.Query())
	request.Limit = 1000
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="local-business-directory.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"id", "name", "primary_category", "categories", "street", "city", "region", "postal_code", "country", "latitude", "longitude", "phone", "email", "website", "opening_hours", "source_url", "source", "license", "updated_at"})
	for offset := 0; ; offset += 1000 {
		businesses, _, err := searchBusinesses(r.Context(), app.pool, request, offset)
		if err != nil {
			return
		}
		for _, business := range businesses {
			_ = writer.Write([]string{
				business.ID, business.Name, business.PrimaryCategory, strings.Join(business.Categories, ";"), business.Street,
				business.City, business.Region, business.PostalCode, business.Country,
				strconv.FormatFloat(business.Latitude, 'f', 6, 64), strconv.FormatFloat(business.Longitude, 'f', 6, 64),
				business.Phone, business.Email, business.Website, business.OpeningHours, business.SourceURL, business.Source, business.License, business.UpdatedAt.Format(time.RFC3339),
			})
		}
		writer.Flush()
		if len(businesses) < 1000 || writer.Error() != nil {
			return
		}
	}
}
