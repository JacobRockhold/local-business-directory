package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS plugin_settings (
  key text PRIMARY KEY,
  value text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO plugin_settings (key, value) VALUES
  ('overpass_endpoint', 'https://overpass-api.de/api/interpreter'),
  ('min_interval_seconds', '5')
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS discovery_jobs (
  id text PRIMARY KEY,
  name text NOT NULL,
  status text NOT NULL CHECK (status IN ('queued','running','completed','failed','canceled')),
  areas jsonb NOT NULL,
  groups jsonb NOT NULL,
  total_steps integer NOT NULL,
  completed_steps integer NOT NULL DEFAULT 0,
  records_seen integer NOT NULL DEFAULT 0,
  records_created integer NOT NULL DEFAULT 0,
  records_updated integer NOT NULL DEFAULT 0,
  last_error text NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  request_id text NOT NULL DEFAULT '',
  automated_caller_plugin text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  completed_at timestamptz
);
ALTER TABLE discovery_jobs ADD COLUMN IF NOT EXISTS error_code text NOT NULL DEFAULT '';
ALTER TABLE discovery_jobs ADD COLUMN IF NOT EXISTS request_id text NOT NULL DEFAULT '';
ALTER TABLE discovery_jobs ADD COLUMN IF NOT EXISTS automated_caller_plugin text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS discovery_jobs_status_created_idx ON discovery_jobs (status, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS discovery_jobs_one_active_automatic_per_caller_idx
  ON discovery_jobs (automated_caller_plugin)
  WHERE automated_caller_plugin <> '' AND status IN ('queued','running');

CREATE TABLE IF NOT EXISTS discovery_requests (
  caller_plugin text NOT NULL,
  request_id text NOT NULL,
  payload_hash text NOT NULL,
  job_id text NOT NULL REFERENCES discovery_jobs(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (caller_plugin, request_id),
  UNIQUE (job_id)
);
CREATE INDEX IF NOT EXISTS discovery_requests_created_idx ON discovery_requests (created_at);

CREATE TABLE IF NOT EXISTS businesses (
  id text PRIMARY KEY,
  source text NOT NULL,
  source_type text NOT NULL,
  source_id text NOT NULL,
  source_url text NOT NULL,
  name text NOT NULL,
  categories text[] NOT NULL DEFAULT '{}',
  primary_category text NOT NULL,
  latitude double precision NOT NULL,
  longitude double precision NOT NULL,
  street text NOT NULL DEFAULT '',
  city text NOT NULL DEFAULT '',
  region text NOT NULL DEFAULT '',
  postal_code text NOT NULL DEFAULT '',
  country text NOT NULL DEFAULT '',
  phone text NOT NULL DEFAULT '',
  email text NOT NULL DEFAULT '',
  website text NOT NULL DEFAULT '',
  opening_hours text NOT NULL DEFAULT '',
  tags jsonb NOT NULL DEFAULT '{}',
  version bigint NOT NULL DEFAULT 1,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  discovery_job_id text NOT NULL DEFAULT '',
  license text NOT NULL DEFAULT 'OpenStreetMap contributors, ODbL 1.0'
);
CREATE UNIQUE INDEX IF NOT EXISTS businesses_source_identity_idx ON businesses (source, source_type, source_id);
CREATE INDEX IF NOT EXISTS businesses_name_idx ON businesses (lower(name));
CREATE INDEX IF NOT EXISTS businesses_location_idx ON businesses (lower(city), lower(region), lower(country));
CREATE INDEX IF NOT EXISTS businesses_updated_idx ON businesses (updated_at, id);
CREATE INDEX IF NOT EXISTS businesses_categories_idx ON businesses USING gin (categories);
`

func migrateDatabase(ctx context.Context, databaseURL string) error {
	if strings.TrimSpace(databaseURL) == "" {
		return errors.New("DATABASE_URL is required")
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	_, err = conn.Exec(ctx, `UPDATE discovery_jobs SET status='queued', last_error='Resumed after plugin restart.', error_code='', started_at=NULL WHERE status='running'`)
	return err
}

func settings(ctx context.Context, pool *pgxpool.Pool) (DirectorySettings, error) {
	result := DirectorySettings{OverpassEndpoint: "https://overpass-api.de/api/interpreter", MinIntervalSecond: 5}
	rows, err := pool.Query(ctx, `SELECT key,value FROM plugin_settings WHERE key = ANY($1)`, []string{"overpass_endpoint", "min_interval_seconds"})
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return result, err
		}
		switch key {
		case "overpass_endpoint":
			result.OverpassEndpoint = value
		case "min_interval_seconds":
			_, _ = fmt.Sscanf(value, "%d", &result.MinIntervalSecond)
		}
	}
	return result, rows.Err()
}

func saveSettings(ctx context.Context, pool *pgxpool.Pool, value DirectorySettings) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for key, setting := range map[string]string{
		"overpass_endpoint":    value.OverpassEndpoint,
		"min_interval_seconds": fmt.Sprintf("%d", value.MinIntervalSecond),
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_settings (key,value,updated_at) VALUES ($1,$2,now()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, key, setting); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func createJob(ctx context.Context, pool *pgxpool.Pool, job DiscoveryJob) error {
	areas, _ := json.Marshal(job.Areas)
	groups, _ := json.Marshal(job.Groups)
	_, err := pool.Exec(ctx, `INSERT INTO discovery_jobs (id,name,status,areas,groups,total_steps,request_id,automated_caller_plugin,created_by,created_at) VALUES ($1,$2,'queued',$3,$4,$5,$6,$7,$8,$9)`, job.ID, job.Name, areas, groups, job.TotalSteps, job.RequestID, job.RequestedByPlugin, job.CreatedBy, job.CreatedAt)
	return err
}

var (
	errRequestConflict = errors.New("discovery request id was reused with a different payload")
	errDiscoveryBusy   = errors.New("another automated discovery job is active for this caller")
)

func createAutomatedJob(ctx context.Context, pool *pgxpool.Pool, job DiscoveryJob, payloadHash string) (DiscoveryJob, bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return DiscoveryJob{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, job.RequestedByPlugin); err != nil {
		return DiscoveryJob{}, false, err
	}
	var existingHash, existingJobID string
	err = tx.QueryRow(ctx, `SELECT payload_hash,job_id FROM discovery_requests WHERE caller_plugin=$1 AND request_id=$2`, job.RequestedByPlugin, job.RequestID).Scan(&existingHash, &existingJobID)
	if err == nil {
		if existingHash != payloadHash {
			return DiscoveryJob{}, false, errRequestConflict
		}
		existing, getErr := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM discovery_jobs WHERE id=$1`, existingJobID))
		if getErr != nil {
			return DiscoveryJob{}, false, getErr
		}
		return existing, true, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return DiscoveryJob{}, false, err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM discovery_jobs WHERE automated_caller_plugin=$1 AND status IN ('queued','running'))`, job.RequestedByPlugin).Scan(&active); err != nil {
		return DiscoveryJob{}, false, err
	}
	if active {
		return DiscoveryJob{}, false, errDiscoveryBusy
	}
	areas, _ := json.Marshal(job.Areas)
	groups, _ := json.Marshal(job.Groups)
	if _, err := tx.Exec(ctx, `INSERT INTO discovery_jobs (id,name,status,areas,groups,total_steps,request_id,automated_caller_plugin,created_by,created_at) VALUES ($1,$2,'queued',$3,$4,$5,$6,$7,$8,$9)`, job.ID, job.Name, areas, groups, job.TotalSteps, job.RequestID, job.RequestedByPlugin, job.CreatedBy, job.CreatedAt); err != nil {
		return DiscoveryJob{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO discovery_requests (caller_plugin,request_id,payload_hash,job_id,created_at) VALUES ($1,$2,$3,$4,$5)`, job.RequestedByPlugin, job.RequestID, payloadHash, job.ID, job.CreatedAt); err != nil {
		return DiscoveryJob{}, false, err
	}
	return job, false, tx.Commit(ctx)
}

func claimJob(ctx context.Context, pool *pgxpool.Pool) (DiscoveryJob, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return DiscoveryJob{}, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
SELECT id,name,areas,groups,total_steps,completed_steps,records_seen,records_created,records_updated,last_error,error_code,request_id,automated_caller_plugin,created_by,created_at
FROM discovery_jobs WHERE status='queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`)
	var job DiscoveryJob
	var areas, groups []byte
	if err := row.Scan(&job.ID, &job.Name, &areas, &groups, &job.TotalSteps, &job.CompletedSteps, &job.RecordsSeen, &job.RecordsCreated, &job.RecordsUpdated, &job.LastError, &job.ErrorCode, &job.RequestID, &job.RequestedByPlugin, &job.CreatedBy, &job.CreatedAt); err != nil {
		return DiscoveryJob{}, err
	}
	if err := json.Unmarshal(areas, &job.Areas); err != nil {
		return DiscoveryJob{}, err
	}
	if err := json.Unmarshal(groups, &job.Groups); err != nil {
		return DiscoveryJob{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE discovery_jobs SET status='running',started_at=$2,last_error='' WHERE id=$1`, job.ID, now); err != nil {
		return DiscoveryJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DiscoveryJob{}, err
	}
	job.Status = "running"
	job.StartedAt = &now
	return job, nil
}

func updateJobStep(ctx context.Context, pool *pgxpool.Pool, jobID string, seen, created, updated int) error {
	_, err := pool.Exec(ctx, `UPDATE discovery_jobs SET completed_steps=completed_steps+1,records_seen=records_seen+$2,records_created=records_created+$3,records_updated=records_updated+$4 WHERE id=$1 AND status='running'`, jobID, seen, created, updated)
	return err
}

func finishJob(ctx context.Context, pool *pgxpool.Pool, jobID, status, errorCode, message string) error {
	_, err := pool.Exec(ctx, `UPDATE discovery_jobs SET status=$2,error_code=$3,last_error=$4,completed_at=now() WHERE id=$1`, jobID, status, errorCode, message)
	return err
}

func cancelJob(ctx context.Context, pool *pgxpool.Pool, jobID string) (bool, error) {
	result, err := pool.Exec(ctx, `UPDATE discovery_jobs SET status='canceled',completed_at=now(),error_code='discovery_canceled',last_error='Canceled by user.' WHERE id=$1 AND status IN ('queued','running')`, jobID)
	return result.RowsAffected() == 1, err
}

func retryJob(ctx context.Context, pool *pgxpool.Pool, jobID string) (bool, error) {
	result, err := pool.Exec(ctx, `UPDATE discovery_jobs SET status='queued',last_error='',error_code='',started_at=NULL,completed_at=NULL WHERE id=$1 AND status='failed'`, jobID)
	return result.RowsAffected() == 1, err
}

func jobCanceled(ctx context.Context, pool *pgxpool.Pool, jobID string) bool {
	var status string
	return pool.QueryRow(ctx, `SELECT status FROM discovery_jobs WHERE id=$1`, jobID).Scan(&status) == nil && status == "canceled"
}

func scanJob(row pgx.Row) (DiscoveryJob, error) {
	var job DiscoveryJob
	var areas, groups []byte
	err := row.Scan(&job.ID, &job.Name, &job.Status, &areas, &groups, &job.TotalSteps, &job.CompletedSteps, &job.RecordsSeen, &job.RecordsCreated, &job.RecordsUpdated, &job.LastError, &job.ErrorCode, &job.RequestID, &job.RequestedByPlugin, &job.CreatedBy, &job.CreatedAt, &job.StartedAt, &job.CompletedAt)
	if err != nil {
		return job, err
	}
	err = json.Unmarshal(areas, &job.Areas)
	if err == nil {
		err = json.Unmarshal(groups, &job.Groups)
	}
	return job, err
}

const jobColumns = `id,name,status,areas,groups,total_steps,completed_steps,records_seen,records_created,records_updated,last_error,error_code,request_id,automated_caller_plugin,created_by,created_at,started_at,completed_at`

func listJobs(ctx context.Context, pool *pgxpool.Pool, limit int) ([]DiscoveryJob, error) {
	rows, err := pool.Query(ctx, `SELECT `+jobColumns+` FROM discovery_jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DiscoveryJob{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func getJob(ctx context.Context, pool *pgxpool.Pool, id string) (DiscoveryJob, error) {
	return scanJob(pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM discovery_jobs WHERE id=$1`, id))
}

func getAutomatedJob(ctx context.Context, pool *pgxpool.Pool, callerPlugin, requestID string) (DiscoveryJob, error) {
	return scanJob(pool.QueryRow(ctx, `SELECT `+prefixedJobColumns("j")+` FROM discovery_jobs j JOIN discovery_requests r ON r.job_id=j.id WHERE r.caller_plugin=$1 AND r.request_id=$2`, callerPlugin, requestID))
}

func prefixedJobColumns(alias string) string {
	parts := strings.Split(jobColumns, ",")
	for index := range parts {
		parts[index] = alias + "." + parts[index]
	}
	return strings.Join(parts, ",")
}

func upsertBusinesses(ctx context.Context, pool *pgxpool.Pool, jobID string, businesses []Business) (int, int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	created, updated := 0, 0
	for _, business := range businesses {
		tags, _ := json.Marshal(business.Tags)
		var inserted bool
		err := tx.QueryRow(ctx, `
INSERT INTO businesses (id,source,source_type,source_id,source_url,name,categories,primary_category,latitude,longitude,street,city,region,postal_code,country,phone,email,website,opening_hours,tags,first_seen_at,last_seen_at,updated_at,discovery_job_id,license)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,now(),now(),now(),$21,$22)
ON CONFLICT (id) DO UPDATE SET source_url=EXCLUDED.source_url,name=EXCLUDED.name,categories=EXCLUDED.categories,primary_category=EXCLUDED.primary_category,latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,street=EXCLUDED.street,city=EXCLUDED.city,region=EXCLUDED.region,postal_code=EXCLUDED.postal_code,country=EXCLUDED.country,phone=EXCLUDED.phone,email=EXCLUDED.email,website=EXCLUDED.website,opening_hours=EXCLUDED.opening_hours,tags=EXCLUDED.tags,version=businesses.version+1,last_seen_at=now(),updated_at=now(),discovery_job_id=EXCLUDED.discovery_job_id,license=EXCLUDED.license
RETURNING (xmax = 0)`, business.ID, business.Source, business.SourceType, business.SourceID, business.SourceURL, business.Name, business.Categories, business.PrimaryCategory, business.Latitude, business.Longitude, business.Street, business.City, business.Region, business.PostalCode, business.Country, business.Phone, business.Email, business.Website, business.OpeningHours, string(tags), jobID, business.License).Scan(&inserted)
		if err != nil {
			return 0, 0, err
		}
		if inserted {
			created++
		} else {
			updated++
		}
	}
	return created, updated, tx.Commit(ctx)
}

func scanBusiness(row pgx.Row) (Business, error) {
	var business Business
	var tags []byte
	err := row.Scan(&business.ID, &business.Source, &business.SourceType, &business.SourceID, &business.SourceURL, &business.Name, &business.Categories, &business.PrimaryCategory, &business.Latitude, &business.Longitude, &business.Street, &business.City, &business.Region, &business.PostalCode, &business.Country, &business.Phone, &business.Email, &business.Website, &business.OpeningHours, &tags, &business.Version, &business.FirstSeenAt, &business.LastSeenAt, &business.UpdatedAt, &business.DiscoveryJobID, &business.License)
	if err == nil {
		err = json.Unmarshal(tags, &business.Tags)
	}
	return business, err
}

const businessColumns = `id,source,source_type,source_id,source_url,name,categories,primary_category,latitude,longitude,street,city,region,postal_code,country,phone,email,website,opening_hours,tags,version,first_seen_at,last_seen_at,updated_at,discovery_job_id,license`

func getBusiness(ctx context.Context, pool *pgxpool.Pool, id string) (Business, error) {
	return scanBusiness(pool.QueryRow(ctx, `SELECT `+businessColumns+` FROM businesses WHERE id=$1`, id))
}

func directoryStats(ctx context.Context, pool *pgxpool.Pool) (map[string]any, error) {
	var total, cities, categories int64
	var lastUpdated *time.Time
	err := pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT NULLIF(lower(city),'')),count(DISTINCT primary_category),max(updated_at) FROM businesses`).Scan(&total, &cities, &categories, &lastUpdated)
	if err != nil {
		return nil, err
	}
	return map[string]any{"businesses": total, "cities": cities, "categories": categories, "lastUpdatedAt": lastUpdated}, nil
}
