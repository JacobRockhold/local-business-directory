package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func exportData(ctx context.Context, databaseURL string, output io.Writer) error {
	if err := migrateDatabase(ctx, databaseURL); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	configuration, err := settings(ctx, pool)
	if err != nil {
		return err
	}
	header := map[string]any{
		"pluginId": pluginID, "pluginVersion": version, "exportFormatVersion": 1, "createdAt": time.Now().UTC(),
		"source": map[string]string{
			"name": "OpenStreetMap", "attribution": "© OpenStreetMap contributors",
			"license": "Open Data Commons Open Database License 1.0", "licenseUrl": "https://www.openstreetmap.org/copyright",
		},
		"settings": configuration,
	}
	headerJSON, _ := json.Marshal(header)
	writer := bufio.NewWriter(output)
	if _, err := writer.Write(headerJSON[:len(headerJSON)-1]); err != nil {
		return err
	}
	if _, err := writer.WriteString(`,"businesses":[`); err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `SELECT `+businessColumns+` FROM businesses ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	encoder := json.NewEncoder(writer)
	first := true
	for rows.Next() {
		business, err := scanBusiness(rows)
		if err != nil {
			return err
		}
		if !first {
			if _, err := writer.WriteString(","); err != nil {
				return err
			}
		}
		first = false
		if err := encoder.Encode(business); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := writer.WriteString("]}\n"); err != nil {
		return err
	}
	return writer.Flush()
}

func importData(ctx context.Context, databaseURL string, input io.Reader) error {
	if err := migrateDatabase(ctx, databaseURL); err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	decoder := json.NewDecoder(io.LimitReader(input, 2<<30))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("import must be a JSON object")
	}
	format := 0
	var importedSettings *DirectorySettings
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, _ := keyToken.(string)
		switch key {
		case "exportFormatVersion":
			if err := decoder.Decode(&format); err != nil {
				return err
			}
		case "settings":
			var value DirectorySettings
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			importedSettings = &value
		case "businesses":
			opening, err := decoder.Token()
			if err != nil || opening != json.Delim('[') {
				return errors.New("businesses must be an array")
			}
			for decoder.More() {
				var business Business
				if err := decoder.Decode(&business); err != nil {
					return fmt.Errorf("decode business: %w", err)
				}
				if err := validateImportedBusiness(business); err != nil {
					return err
				}
				tags, _ := json.Marshal(business.Tags)
				if business.Version < 1 {
					business.Version = 1
				}
				if business.FirstSeenAt.IsZero() {
					business.FirstSeenAt = time.Now().UTC()
				}
				if business.LastSeenAt.IsZero() {
					business.LastSeenAt = business.FirstSeenAt
				}
				if business.UpdatedAt.IsZero() {
					business.UpdatedAt = business.LastSeenAt
				}
				_, err := tx.Exec(ctx, `
INSERT INTO businesses (`+businessColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
ON CONFLICT (id) DO UPDATE SET source=EXCLUDED.source,source_type=EXCLUDED.source_type,source_id=EXCLUDED.source_id,source_url=EXCLUDED.source_url,name=EXCLUDED.name,categories=EXCLUDED.categories,primary_category=EXCLUDED.primary_category,latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,street=EXCLUDED.street,city=EXCLUDED.city,region=EXCLUDED.region,postal_code=EXCLUDED.postal_code,country=EXCLUDED.country,phone=EXCLUDED.phone,email=EXCLUDED.email,website=EXCLUDED.website,opening_hours=EXCLUDED.opening_hours,tags=EXCLUDED.tags,version=EXCLUDED.version,first_seen_at=EXCLUDED.first_seen_at,last_seen_at=EXCLUDED.last_seen_at,updated_at=EXCLUDED.updated_at,discovery_job_id=EXCLUDED.discovery_job_id,license=EXCLUDED.license`,
					business.ID, business.Source, business.SourceType, business.SourceID, business.SourceURL, business.Name, business.Categories, business.PrimaryCategory, business.Latitude, business.Longitude, business.Street, business.City, business.Region, business.PostalCode, business.Country, business.Phone, business.Email, business.Website, business.OpeningHours, string(tags), business.Version, business.FirstSeenAt, business.LastSeenAt, business.UpdatedAt, business.DiscoveryJobID, business.License)
				if err != nil {
					return err
				}
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
				return errors.New("businesses array is incomplete")
			}
		default:
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if format != 1 {
		return fmt.Errorf("unsupported export format version %d", format)
	}
	if importedSettings != nil {
		if err := validateEndpoint(importedSettings.OverpassEndpoint); err != nil {
			return fmt.Errorf("imported endpoint: %w", err)
		}
		if importedSettings.MinIntervalSecond < 1 || importedSettings.MinIntervalSecond > 60 {
			return errors.New("imported request interval is outside 1 to 60 seconds")
		}
		for key, value := range map[string]string{"overpass_endpoint": importedSettings.OverpassEndpoint, "min_interval_seconds": fmt.Sprintf("%d", importedSettings.MinIntervalSecond)} {
			if _, err := tx.Exec(ctx, `INSERT INTO plugin_settings (key,value,updated_at) VALUES ($1,$2,now()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, key, value); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func validateImportedBusiness(business Business) error {
	if !strings.HasPrefix(business.ID, "osm:") || business.Source != "openstreetmap" || strings.TrimSpace(business.Name) == "" {
		return fmt.Errorf("import contains an invalid business record %q", business.ID)
	}
	if business.Latitude < -90 || business.Latitude > 90 || business.Longitude < -180 || business.Longitude > 180 {
		return fmt.Errorf("import contains invalid coordinates for %q", business.ID)
	}
	return nil
}
