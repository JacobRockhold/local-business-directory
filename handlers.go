package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
		Region: values.Get("region"), Country: values.Get("country"), DiscoveryJobID: values.Get("discoveryJobId"), Limit: limit,
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
	_, ok := callerPluginID(w, r)
	return ok
}

func callerPluginID(w http.ResponseWriter, r *http.Request) (string, bool) {
	caller := strings.TrimSpace(r.Header.Get("X-Hub-Caller-Plugin"))
	if caller == "" {
		writeError(w, http.StatusUnauthorized, "hub_bridge_required", "This capability is available only through the approved FileForge bridge.")
		return "", false
	}
	return caller, true
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
	if len(strings.TrimSpace(request.DiscoveryJobID)) > 128 {
		writeError(w, http.StatusUnprocessableEntity, "search_invalid", "discoveryJobId must be 128 characters or fewer.")
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

func (app *App) capabilityDiscover(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	caller, ok := callerPluginID(w, r)
	if !ok {
		return
	}
	var request DiscoveryRequestInput
	if err := decodeJSON(w, r, &request, 64<<10); err != nil {
		logAutomatedDiscovery(caller, request.RequestID, "", 0, 0, 0, "invalid", started)
		writeError(w, http.StatusUnprocessableEntity, "discovery_invalid", err.Error())
		return
	}
	groups, err := validateAutomatedDiscoveryRequest(&request)
	if err != nil {
		logAutomatedDiscovery(caller, request.RequestID, "", 0, 0, 0, "invalid", started)
		writeError(w, http.StatusUnprocessableEntity, "discovery_invalid", err.Error())
		return
	}
	request.Groups = groups
	job := DiscoveryJob{
		ID: newID("job"), RequestID: request.RequestID, RequestedByPlugin: caller,
		Name: request.Name, Status: "queued", Areas: request.Areas, Groups: groups,
		TotalSteps: len(request.Areas) * len(groups), CreatedBy: "plugin:" + caller, CreatedAt: time.Now().UTC(),
	}
	created, deduplicated, err := createAutomatedJob(r.Context(), app.pool, job, discoveryPayloadHash(request))
	if errors.Is(err, errRequestConflict) {
		logAutomatedDiscovery(caller, request.RequestID, "", 0, 0, 0, "request_conflict", started)
		writeError(w, http.StatusConflict, "request_conflict", "That requestId was already used with different discovery parameters.")
		return
	}
	if errors.Is(err, errDiscoveryBusy) {
		logAutomatedDiscovery(caller, request.RequestID, "", 0, 0, 0, "discovery_busy", started)
		writeError(w, http.StatusConflict, "discovery_busy", "This plugin already has an automated discovery job queued or running.")
		return
	}
	if err != nil {
		logAutomatedDiscovery(caller, request.RequestID, "", 0, 0, 0, "unavailable", started)
		writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "The discovery request could not be queued.")
		return
	}
	logAutomatedDiscovery(caller, request.RequestID, created.ID, created.RecordsSeen, created.RecordsCreated, created.RecordsUpdated, created.Status, started)
	writeJSON(w, http.StatusAccepted, map[string]any{"job": queuedDiscoverySummary(created), "deduplicated": deduplicated})
}

func (app *App) capabilityDiscoveryStatus(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerPluginID(w, r)
	if !ok {
		return
	}
	var request DiscoveryStatusRequest
	if err := decodeJSON(w, r, &request, 32<<10); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "status_invalid", err.Error())
		return
	}
	request.RequestID = strings.TrimSpace(request.RequestID)
	if len(request.RequestID) < 16 || len(request.RequestID) > 128 {
		writeError(w, http.StatusUnprocessableEntity, "status_invalid", "requestId must contain between 16 and 128 characters.")
		return
	}
	job, err := getAutomatedJob(r.Context(), app.pool, caller, request.RequestID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "discovery_not_found", "That discovery request was not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "Discovery status is temporarily unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": statusDiscoverySummary(job)})
}

func validateAutomatedDiscoveryRequest(request *DiscoveryRequestInput) ([]string, error) {
	request.RequestID = strings.TrimSpace(request.RequestID)
	if len(request.RequestID) < 16 || len(request.RequestID) > 128 {
		return nil, errors.New("requestId must contain between 16 and 128 characters")
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 120 {
		return nil, errors.New("name must contain between 1 and 120 characters")
	}
	if len(request.Areas) == 0 || len(request.Areas) > 50 {
		return nil, errors.New("areas must contain between 1 and 50 entries")
	}
	for index := range request.Areas {
		request.Areas[index].Label = strings.TrimSpace(request.Areas[index].Label)
		if err := validateArea(request.Areas[index]); err != nil {
			return nil, err
		}
	}
	if len(request.Groups) == 0 {
		return nil, errors.New("groups must contain at least one supported discovery group")
	}
	if len(request.Areas)*len(request.Groups) > 350 {
		return nil, errors.New("areas multiplied by groups must not exceed 350 discovery steps")
	}
	groups, err := validateGroups(request.Groups)
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func discoveryPayloadHash(request DiscoveryRequestInput) string {
	payload, _ := json.Marshal(request)
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func queuedDiscoverySummary(job DiscoveryJob) map[string]any {
	return map[string]any{
		"id": job.ID, "requestId": job.RequestID, "status": job.Status, "name": job.Name,
		"totalSteps": job.TotalSteps, "createdAt": job.CreatedAt,
	}
}

func statusDiscoverySummary(job DiscoveryJob) map[string]any {
	result := map[string]any{
		"id": job.ID, "requestId": job.RequestID, "status": job.Status,
		"totalSteps": job.TotalSteps, "completedSteps": job.CompletedSteps,
		"recordsSeen": job.RecordsSeen, "recordsCreated": job.RecordsCreated, "recordsUpdated": job.RecordsUpdated,
		"createdAt": job.CreatedAt, "errorCode": job.ErrorCode,
	}
	if job.StartedAt != nil {
		result["startedAt"] = *job.StartedAt
	}
	if job.CompletedAt != nil {
		result["completedAt"] = *job.CompletedAt
	}
	return result
}

func logAutomatedDiscovery(caller, requestID, jobID string, seen, created, updated int, status string, started time.Time) {
	log.Printf("automated_discovery caller=%q request=%q job=%q records_seen=%d records_created=%d records_updated=%d status=%q duration_ms=%d", caller, requestID, jobID, seen, created, updated, status, time.Since(started).Milliseconds())
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
