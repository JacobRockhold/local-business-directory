package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func searchWhere(request SearchRequest) (string, []any) {
	clauses := []string{"TRUE"}
	arguments := []any{}
	add := func(clause string, value any) {
		arguments = append(arguments, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(arguments)))
	}
	if value := strings.TrimSpace(request.Query); value != "" {
		arguments = append(arguments, "%"+value+"%")
		placeholder := len(arguments)
		clauses = append(clauses, fmt.Sprintf(`(name ILIKE $%d OR city ILIKE $%d OR website ILIKE $%d)`, placeholder, placeholder, placeholder))
	}
	if len(request.Categories) > 0 {
		add(`categories && $%d`, request.Categories)
	}
	if value := strings.TrimSpace(request.City); value != "" {
		add(`city ILIKE $%d`, value)
	}
	if value := strings.TrimSpace(request.Region); value != "" {
		add(`region ILIKE $%d`, value)
	}
	if value := strings.TrimSpace(request.Country); value != "" {
		add(`country ILIKE $%d`, value)
	}
	return strings.Join(clauses, " AND "), arguments
}

func searchBusinesses(ctx context.Context, pool *pgxpool.Pool, request SearchRequest, offset int) ([]Business, int, error) {
	limit := request.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	where, arguments := searchWhere(request)
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM businesses WHERE `+where, arguments...).Scan(&total); err != nil {
		return nil, 0, err
	}
	arguments = append(arguments, limit, offset)
	query := `SELECT ` + businessColumns + ` FROM businesses WHERE ` + where + fmt.Sprintf(` ORDER BY lower(name),id LIMIT $%d OFFSET $%d`, len(arguments)-1, len(arguments))
	rows, err := pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []Business{}
	for rows.Next() {
		business, err := scanBusiness(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, business)
	}
	return result, total, rows.Err()
}

func encodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeOffsetCursor(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return offset, nil
}

type backfillCursor struct {
	SnapshotTime time.Time `json:"snapshotTime"`
	UpdatedAt    time.Time `json:"updatedAt"`
	ID           string    `json:"id"`
}

func encodeBackfillCursor(cursor backfillCursor) string {
	contents, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(contents)
}

func decodeBackfillCursor(value string) (backfillCursor, error) {
	if value == "" {
		return backfillCursor{SnapshotTime: time.Now().UTC(), UpdatedAt: time.Unix(0, 0).UTC()}, nil
	}
	contents, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return backfillCursor{}, fmt.Errorf("invalid backfill cursor")
	}
	var cursor backfillCursor
	if err := json.Unmarshal(contents, &cursor); err != nil || cursor.SnapshotTime.IsZero() || cursor.UpdatedAt.IsZero() {
		return backfillCursor{}, fmt.Errorf("invalid backfill cursor")
	}
	return cursor, nil
}

func backfillBusinesses(ctx context.Context, pool *pgxpool.Pool, request BackfillRequest) (map[string]any, error) {
	limit := request.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	cursor, err := decodeBackfillCursor(request.Cursor)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT `+businessColumns+` FROM businesses WHERE updated_at <= $1 AND (updated_at,id) > ($2,$3) ORDER BY updated_at,id LIMIT $4`, cursor.SnapshotTime, cursor.UpdatedAt, cursor.ID, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	businesses := []Business{}
	for rows.Next() {
		business, err := scanBusiness(rows)
		if err != nil {
			return nil, err
		}
		businesses = append(businesses, business)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	complete := len(businesses) <= limit
	if !complete {
		businesses = businesses[:limit]
	}
	records := make([]map[string]any, 0, len(businesses))
	for _, business := range businesses {
		records = append(records, map[string]any{"id": business.ID, "version": strconv.FormatInt(business.Version, 10), "updatedAt": business.UpdatedAt, "deleted": false, "data": business})
	}
	result := map[string]any{
		"capability":   "com.businesshub.local-businesses.directory.v1",
		"snapshotTime": cursor.SnapshotTime,
		"records":      records,
		"complete":     complete,
	}
	if !complete && len(businesses) > 0 {
		last := businesses[len(businesses)-1]
		result["nextCursor"] = encodeBackfillCursor(backfillCursor{SnapshotTime: cursor.SnapshotTime, UpdatedAt: last.UpdatedAt, ID: last.ID})
	}
	return result, nil
}
