package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const pluginID = "com.businesshub.local-businesses"
const pluginName = "Local Business Directory"

var version = "0.2.0"

type App struct {
	pool *pgxpool.Pool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	databaseURL := os.Getenv("DATABASE_URL")
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "migrate":
			err = migrateDatabase(ctx, databaseURL)
		case "export":
			err = exportData(ctx, databaseURL, os.Stdout)
		case "import":
			err = importData(ctx, databaseURL, os.Stdin)
		default:
			err = fmt.Errorf("unknown command %q", os.Args[1])
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := migrateDatabase(ctx, databaseURL); err != nil {
		log.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	app := &App{pool: pool}

	worker := &DiscoveryWorker{pool: pool, overpass: NewOverpassClient(), http: &http.Client{Timeout: 20 * time.Second}}
	go worker.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_hub/health/live", app.live)
	mux.HandleFunc("GET /_hub/health/ready", app.ready)
	mux.HandleFunc("GET /_hub/metadata", app.metadata)
	mux.HandleFunc("POST /_hub/events/deliver", app.deliverEvent)
	mux.HandleFunc("POST /_hub/capabilities/com.businesshub.local-businesses.directory.v1/search", app.capabilitySearch)
	mux.HandleFunc("POST /_hub/capabilities/com.businesshub.local-businesses.directory.v1/backfill", app.capabilityBackfill)
	mux.HandleFunc("POST /_hub/capabilities/com.businesshub.local-businesses.directory.v1/get", app.capabilityGet)
	mux.HandleFunc("POST /_hub/capabilities/com.businesshub.local-businesses.directory.v1/discover", app.capabilityDiscover)
	mux.HandleFunc("POST /_hub/capabilities/com.businesshub.local-businesses.directory.v1/discovery-status", app.capabilityDiscoveryStatus)
	mux.HandleFunc("GET /api/stats", app.stats)
	mux.HandleFunc("GET /api/businesses", app.listBusinesses)
	mux.HandleFunc("GET /api/businesses.csv", app.exportCSV)
	mux.HandleFunc("GET /api/businesses/{id}", app.business)
	mux.HandleFunc("GET /api/discovery-jobs", app.jobs)
	mux.HandleFunc("GET /api/discovery-jobs/{id}", app.job)
	mux.HandleFunc("POST /api/discovery-jobs", app.createDiscoveryJob)
	mux.HandleFunc("POST /api/discovery-jobs/{id}/cancel", app.cancelDiscoveryJob)
	mux.HandleFunc("POST /api/discovery-jobs/{id}/retry", app.retryDiscoveryJob)
	mux.HandleFunc("GET /api/settings", app.getSettings)
	mux.HandleFunc("PUT /api/settings", app.putSettings)
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir("/ui"))))
	mux.HandleFunc("GET /", app.index)

	server := &http.Server{
		Addr: ":8080", Handler: securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("%s %s listening", pluginID, version)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (app *App) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

func (app *App) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := app.pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Directory storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (app *App) metadata(w http.ResponseWriter, _ *http.Request) {
	id := os.Getenv("HUB_PLUGIN_ID")
	if id == "" {
		id = pluginID
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "name": pluginName, "version": version})
}

func (app *App) deliverEvent(w http.ResponseWriter, r *http.Request) {
	var delivery map[string]any
	if err := decodeJSON(w, r, &delivery, 1<<20); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "delivery_invalid", err.Error())
		return
	}
	// This plugin declares no subscriptions. The endpoint remains available to
	// satisfy Plugin API v1 and safely acknowledges accidental empty delivery.
	w.WriteHeader(http.StatusAccepted)
}

func (app *App) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<!doctype html><html><body><h1>"+pluginName+"</h1><p>Open this application inside FileForge.</p></body></html>")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, limit int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request must contain one JSON document")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func newID(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return prefix + "_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(value)
}

func correlationID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Correlation-ID")); len(value) >= 16 && len(value) <= 128 {
		return value
	}
	return newID("cor")
}
