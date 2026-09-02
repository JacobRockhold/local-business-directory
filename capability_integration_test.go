package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testSchemaCounter atomic.Uint64

func isolatedTestDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("lbd_test_%d_%d", time.Now().UnixNano(), testSchemaCounter.Add(1))
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := base.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		base.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = base.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		base.Close()
	})
	config, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	scopedURL := config.ConnString()
	if err := migrateDatabase(ctx, scopedURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, scopedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return scopedURL, pool
}

func territoryRequest(requestID string) DiscoveryRequestInput {
	areas := make([]Area, 7)
	for index := range areas {
		areas[index] = Area{Label: fmt.Sprintf("Territory %d", index+1), Latitude: 38.7628 + float64(index)/100, Longitude: -93.7361, RadiusMeters: 25000}
	}
	return DiscoveryRequestInput{RequestID: requestID, Name: "B2B CRM weekly prospect discovery", Areas: areas, Groups: []string{"shop", "craft", "office"}}
}

func callCapability(t *testing.T, handler http.HandlerFunc, payload any, caller string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewReader(body))
	if caller != "" {
		request.Header.Set("X-Hub-Caller-Plugin", caller)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func TestAutomatedDiscoveryIdempotencyBusyAndOwnership(t *testing.T) {
	_, pool := isolatedTestDatabase(t)
	app := &App{pool: pool}
	caller := "com.businesshub.crm"
	request := territoryRequest("disc_01JCRM_AUTOMATIC_WEEKLY_20260907")

	first := callCapability(t, app.capabilityDiscover, request, caller)
	if first.Code != http.StatusAccepted {
		t.Fatalf("expected discovery acceptance, received %d: %s", first.Code, first.Body.String())
	}
	var firstResult struct {
		Job struct {
			ID         string `json:"id"`
			TotalSteps int    `json:"totalSteps"`
		} `json:"job"`
		Deduplicated bool `json:"deduplicated"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstResult); err != nil {
		t.Fatal(err)
	}
	if firstResult.Deduplicated || firstResult.Job.ID == "" || firstResult.Job.TotalSteps != 21 {
		t.Fatalf("unexpected first response: %s", first.Body.String())
	}

	retry := callCapability(t, app.capabilityDiscover, request, caller)
	var retryResult struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
		Deduplicated bool `json:"deduplicated"`
	}
	if retry.Code != http.StatusAccepted || json.Unmarshal(retry.Body.Bytes(), &retryResult) != nil || !retryResult.Deduplicated || retryResult.Job.ID != firstResult.Job.ID {
		t.Fatalf("expected identical retry to return the same job: %d %s", retry.Code, retry.Body.String())
	}

	changed := request
	changed.Name = "Changed payload"
	conflict := callCapability(t, app.capabilityDiscover, changed, caller)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "request_conflict") {
		t.Fatalf("expected request conflict, received %d: %s", conflict.Code, conflict.Body.String())
	}

	busyRequest := territoryRequest("disc_01JCRM_AUTOMATIC_WEEKLY_20260908")
	busy := callCapability(t, app.capabilityDiscover, busyRequest, caller)
	if busy.Code != http.StatusConflict || !strings.Contains(busy.Body.String(), "discovery_busy") {
		t.Fatalf("expected discovery busy, received %d: %s", busy.Code, busy.Body.String())
	}

	status := callCapability(t, app.capabilityDiscoveryStatus, DiscoveryStatusRequest{RequestID: request.RequestID}, caller)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), firstResult.Job.ID) {
		t.Fatalf("expected owner status, received %d: %s", status.Code, status.Body.String())
	}
	foreign := callCapability(t, app.capabilityDiscoveryStatus, DiscoveryStatusRequest{RequestID: request.RequestID}, "com.businesshub.other")
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("expected foreign caller to receive not found, received %d: %s", foreign.Code, foreign.Body.String())
	}
}

func TestDiscoveryJobFilter(t *testing.T) {
	_, pool := isolatedTestDatabase(t)
	now := time.Now().UTC()
	area := []Area{{Label: "Test", Latitude: 38.7628, Longitude: -93.7361, RadiusMeters: 250}}
	for _, id := range []string{"job_first", "job_second"} {
		if err := createJob(context.Background(), pool, DiscoveryJob{ID: id, Name: id, Status: "queued", Areas: area, Groups: []string{"shop"}, TotalSteps: 1, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for index, jobID := range []string{"job_first", "job_second"} {
		business := Business{ID: fmt.Sprintf("osm:node:%d", index+1), Source: "openstreetmap", SourceType: "node", SourceID: fmt.Sprint(index + 1), SourceURL: fmt.Sprintf("https://www.openstreetmap.org/node/%d", index+1), Name: fmt.Sprintf("Business %d", index+1), Categories: []string{"shop:yes"}, PrimaryCategory: "shop:yes", Latitude: 38.7, Longitude: -93.7, Tags: map[string]string{"shop": "yes"}, License: "OpenStreetMap contributors, ODbL 1.0"}
		if _, _, err := upsertBusinesses(context.Background(), pool, jobID, []Business{business}); err != nil {
			t.Fatal(err)
		}
	}
	businesses, total, err := searchBusinesses(context.Background(), pool, SearchRequest{DiscoveryJobID: "job_first", Limit: 100}, 0)
	if err != nil || total != 1 || len(businesses) != 1 || businesses[0].DiscoveryJobID != "job_first" {
		t.Fatalf("unexpected filtered search: total=%d businesses=%#v err=%v", total, businesses, err)
	}
}

func TestExportImportRoundTripsDiscoveryRequests(t *testing.T) {
	sourceURL, sourcePool := isolatedTestDatabase(t)
	request := territoryRequest("disc_01JCRM_AUTOMATIC_WEEKLY_20260909")
	groups, err := validateAutomatedDiscoveryRequest(&request)
	if err != nil {
		t.Fatal(err)
	}
	job := DiscoveryJob{ID: "job_export_request", RequestID: request.RequestID, RequestedByPlugin: "com.businesshub.crm", Name: request.Name, Status: "queued", Areas: request.Areas, Groups: groups, TotalSteps: 21, CreatedBy: "plugin:com.businesshub.crm", CreatedAt: time.Now().UTC()}
	if _, _, err := createAutomatedJob(context.Background(), sourcePool, job, discoveryPayloadHash(request)); err != nil {
		t.Fatal(err)
	}
	business := Business{ID: "osm:node:99", Source: "openstreetmap", SourceType: "node", SourceID: "99", SourceURL: "https://www.openstreetmap.org/node/99", Name: "Exported Business", Categories: []string{"shop:yes"}, PrimaryCategory: "shop:yes", Latitude: 38.7, Longitude: -93.7, Tags: map[string]string{"shop": "yes"}, License: "OpenStreetMap contributors, ODbL 1.0"}
	if _, _, err := upsertBusinesses(context.Background(), sourcePool, job.ID, []Business{business}); err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := exportData(context.Background(), sourceURL, &exported); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported.String(), `"exportFormatVersion":2`) || !strings.Contains(exported.String(), `"discoveryRequests"`) {
		t.Fatalf("export does not contain version 2 discovery requests: %s", exported.String())
	}

	destinationURL, destinationPool := isolatedTestDatabase(t)
	if err := importData(context.Background(), destinationURL, bytes.NewReader(exported.Bytes())); err != nil {
		t.Fatal(err)
	}
	var requestCount int
	if err := destinationPool.QueryRow(context.Background(), `SELECT count(*) FROM discovery_requests WHERE caller_plugin=$1 AND request_id=$2 AND job_id=$3`, job.RequestedByPlugin, job.RequestID, job.ID).Scan(&requestCount); err != nil || requestCount != 1 {
		t.Fatalf("discovery request did not round-trip: count=%d err=%v", requestCount, err)
	}
	if imported, err := getBusiness(context.Background(), destinationPool, business.ID); err != nil || imported.DiscoveryJobID != job.ID {
		t.Fatalf("business did not round-trip with job correlation: %#v err=%v", imported, err)
	}
}

func TestUpgradeFromVersion011PreservesData(t *testing.T) {
	databaseURL, pool := isolatedTestDatabase(t)
	ctx := context.Background()
	job := DiscoveryJob{ID: "job_legacy", Name: "Legacy job", Status: "queued", Areas: []Area{{Label: "Legacy", Latitude: 38.7, Longitude: -93.7, RadiusMeters: 250}}, Groups: []string{"shop"}, TotalSteps: 1, CreatedBy: "legacy-user", CreatedAt: time.Now().UTC()}
	if err := createJob(ctx, pool, job); err != nil {
		t.Fatal(err)
	}
	business := Business{ID: "osm:node:101", Source: "openstreetmap", SourceType: "node", SourceID: "101", SourceURL: "https://www.openstreetmap.org/node/101", Name: "Legacy Business", Categories: []string{"shop:yes"}, PrimaryCategory: "shop:yes", Latitude: 38.7, Longitude: -93.7, Tags: map[string]string{"shop": "yes"}, License: "OpenStreetMap contributors, ODbL 1.0"}
	if _, _, err := upsertBusinesses(ctx, pool, job.ID, []Business{business}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE plugin_settings SET value='7' WHERE key='min_interval_seconds'; DROP TABLE discovery_requests; DROP INDEX discovery_jobs_one_active_automatic_per_caller_idx; ALTER TABLE discovery_jobs DROP COLUMN error_code, DROP COLUMN request_id, DROP COLUMN automated_caller_plugin`); err != nil {
		t.Fatal(err)
	}
	if err := migrateDatabase(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	if preserved, err := getJob(ctx, pool, job.ID); err != nil || preserved.Name != job.Name {
		t.Fatalf("legacy job was not preserved: %#v err=%v", preserved, err)
	}
	if preserved, err := getBusiness(ctx, pool, business.ID); err != nil || preserved.Name != business.Name {
		t.Fatalf("legacy business was not preserved: %#v err=%v", preserved, err)
	}
	if configuration, err := settings(ctx, pool); err != nil || configuration.MinIntervalSecond != 7 {
		t.Fatalf("legacy settings were not preserved: %#v err=%v", configuration, err)
	}
}
