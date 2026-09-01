package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var defaultDiscoveryGroups = []string{"shop", "craft", "office", "amenity", "tourism", "healthcare", "leisure"}

var discoveryFilters = map[string]string{
	"shop":       `["shop"]`,
	"craft":      `["craft"]`,
	"office":     `["office"]`,
	"healthcare": `["healthcare"]`,
	"amenity":    `["amenity"~"^(restaurant|cafe|fast_food|bar|pub|biergarten|food_court|ice_cream|bank|bureau_de_change|pharmacy|clinic|dentist|doctors|veterinary|fuel|car_rental|car_wash|vehicle_inspection|cinema|theatre|nightclub|casino|marketplace|childcare|driving_school|language_school|music_school|prep_school|animal_boarding|animal_breeding|studio|coworking_space|internet_cafe)$"]`,
	"tourism":    `["tourism"~"^(hotel|motel|guest_house|hostel|apartment|camp_site|caravan_site|chalet|attraction|museum|gallery|theme_park|zoo|aquarium|information)$"]`,
	"leisure":    `["leisure"~"^(fitness_centre|sports_centre|golf_course|miniature_golf|marina|bowling_alley|dance|escape_game|water_park|amusement_arcade|horse_riding)$"]`,
}

type overpassElement struct {
	Type      string   `json:"type"`
	ID        int64    `json:"id"`
	Latitude  *float64 `json:"lat"`
	Longitude *float64 `json:"lon"`
	Center    *struct {
		Latitude  float64 `json:"lat"`
		Longitude float64 `json:"lon"`
	} `json:"center"`
	Tags map[string]string `json:"tags"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

type OverpassClient struct {
	http *http.Client
}

func NewOverpassClient() *OverpassClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialPublicAddress
	transport.MaxIdleConns = 4
	transport.MaxIdleConnsPerHost = 2
	transport.ResponseHeaderTimeout = 210 * time.Second
	return &OverpassClient{http: &http.Client{Timeout: 4 * time.Minute, Transport: transport}}
}

func validateEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("the Overpass endpoint must be a complete HTTPS URL without credentials, query parameters, or a fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("the Overpass endpoint must not target a local or internal hostname")
	}
	if address := net.ParseIP(host); address != nil && (address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsUnspecified()) {
		return errors.New("the Overpass endpoint must not target a private or local address")
	}
	return nil
}

func dialPublicAddress(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, candidate := range addresses {
		if !isPublicIP(candidate.IP) {
			continue
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("endpoint %s did not resolve to a public address", host)
}

func isPublicIP(address net.IP) bool {
	return address != nil && address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsUnspecified()
}

func buildOverpassQuery(area Area, group string) (string, error) {
	filter, ok := discoveryFilters[group]
	if !ok {
		return "", fmt.Errorf("unsupported discovery group %q", group)
	}
	if err := validateArea(area); err != nil {
		return "", err
	}
	return fmt.Sprintf(`[out:json][timeout:180];
(
  nwr(around:%d,%.6f,%.6f)["name"]%s;
);
out center tags qt;`, area.RadiusMeters, area.Latitude, area.Longitude, filter), nil
}

func (client *OverpassClient) Fetch(ctx context.Context, endpoint string, area Area, group string) ([]Business, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	query, err := buildOverpassQuery(area, group)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt*5) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		values := url.Values{"data": {query}}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "FileForge-Local-Business-Directory/0.1 (+https://github.com/JacobRockhold/business-hub)")
		response, err := client.http.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 96<<20))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			lastErr = fmt.Errorf("Overpass returned %s", response.Status)
			if retryAfter, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil && retryAfter > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(min(retryAfter, 60)) * time.Second):
				}
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("Overpass returned %s: %s", response.Status, strings.TrimSpace(string(body[:min(len(body), 500)])))
		}
		var payload overpassResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode Overpass response: %w", err)
		}
		return transformElements(payload.Elements, group), nil
	}
	return nil, fmt.Errorf("Overpass request failed after retries: %w", lastErr)
}

func transformElements(elements []overpassElement, group string) []Business {
	byID := make(map[string]Business, len(elements))
	for _, element := range elements {
		if strings.TrimSpace(element.Tags["name"]) == "" {
			continue
		}
		latitude, longitude, ok := elementCoordinates(element)
		if !ok {
			continue
		}
		id := fmt.Sprintf("osm:%s:%d", element.Type, element.ID)
		categories := businessCategories(element.Tags)
		primary := group + ":" + element.Tags[group]
		if element.Tags[group] == "" && len(categories) > 0 {
			primary = categories[0]
		}
		street := strings.TrimSpace(strings.Join([]string{element.Tags["addr:housenumber"], element.Tags["addr:street"]}, " "))
		business := Business{
			ID: id, Source: "openstreetmap", SourceType: element.Type, SourceID: strconv.FormatInt(element.ID, 10),
			SourceURL: "https://www.openstreetmap.org/" + element.Type + "/" + strconv.FormatInt(element.ID, 10),
			Name:      element.Tags["name"], Categories: categories, PrimaryCategory: primary,
			Latitude: latitude, Longitude: longitude, Street: street,
			City:       firstTag(element.Tags, "addr:city", "addr:town", "addr:village", "addr:hamlet"),
			Region:     firstTag(element.Tags, "addr:state", "addr:province", "addr:county"),
			PostalCode: element.Tags["addr:postcode"], Country: element.Tags["addr:country"],
			Phone: firstTag(element.Tags, "contact:phone", "phone"), Email: firstTag(element.Tags, "contact:email", "email"),
			Website: firstTag(element.Tags, "contact:website", "website", "url"), OpeningHours: element.Tags["opening_hours"],
			Tags: element.Tags, License: "OpenStreetMap contributors, ODbL 1.0",
		}
		byID[id] = business
	}
	result := make([]Business, 0, len(byID))
	for _, business := range byID {
		result = append(result, business)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func elementCoordinates(element overpassElement) (float64, float64, bool) {
	if element.Latitude != nil && element.Longitude != nil {
		return *element.Latitude, *element.Longitude, true
	}
	if element.Center != nil {
		return element.Center.Latitude, element.Center.Longitude, true
	}
	return 0, 0, false
}

func businessCategories(tags map[string]string) []string {
	result := []string{}
	for _, key := range defaultDiscoveryGroups {
		if value := strings.TrimSpace(tags[key]); value != "" && value != "no" {
			result = append(result, key+":"+value)
		}
	}
	return result
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(tags[key]); value != "" {
			return value
		}
	}
	return ""
}

func validateArea(area Area) error {
	if strings.TrimSpace(area.Label) == "" || len(area.Label) > 120 {
		return errors.New("each area needs a label of 120 characters or fewer")
	}
	if math.IsNaN(area.Latitude) || math.IsInf(area.Latitude, 0) || area.Latitude < -90 || area.Latitude > 90 {
		return fmt.Errorf("area %q has an invalid latitude", area.Label)
	}
	if math.IsNaN(area.Longitude) || math.IsInf(area.Longitude, 0) || area.Longitude < -180 || area.Longitude > 180 {
		return fmt.Errorf("area %q has an invalid longitude", area.Label)
	}
	if area.RadiusMeters < 250 || area.RadiusMeters > 25000 {
		return fmt.Errorf("area %q radius must be between 250 and 25,000 meters", area.Label)
	}
	return nil
}

func validateGroups(groups []string) ([]string, error) {
	if len(groups) == 0 {
		return append([]string(nil), defaultDiscoveryGroups...), nil
	}
	seen := map[string]bool{}
	result := []string{}
	for _, group := range groups {
		group = strings.ToLower(strings.TrimSpace(group))
		if _, ok := discoveryFilters[group]; !ok {
			return nil, fmt.Errorf("unsupported discovery group %q", group)
		}
		if !seen[group] {
			seen[group] = true
			result = append(result, group)
		}
	}
	return result, nil
}

type DiscoveryWorker struct {
	pool     *pgxpool.Pool
	overpass *OverpassClient
	http     *http.Client
}

func (worker *DiscoveryWorker) Run(ctx context.Context) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		job, err := claimJob(ctx, worker.pool)
		if errors.Is(err, pgx.ErrNoRows) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		if err != nil {
			log.Printf("claim discovery job: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		worker.execute(ctx, job)
	}
}

func (worker *DiscoveryWorker) execute(ctx context.Context, job DiscoveryJob) {
	step := 0
	for _, area := range job.Areas {
		for _, group := range job.Groups {
			step++
			if step <= job.CompletedSteps {
				continue
			}
			if jobCanceled(ctx, worker.pool, job.ID) {
				return
			}
			configuration, err := settings(ctx, worker.pool)
			if err != nil {
				worker.fail(ctx, job.ID, err)
				return
			}
			if step > 1 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(configuration.MinIntervalSecond) * time.Second):
				}
			}
			businesses, err := worker.overpass.Fetch(ctx, configuration.OverpassEndpoint, area, group)
			if err != nil {
				worker.fail(ctx, job.ID, fmt.Errorf("%s / %s: %w", area.Label, group, err))
				return
			}
			created, updated, err := upsertBusinesses(ctx, worker.pool, job.ID, businesses)
			if err != nil {
				worker.fail(ctx, job.ID, err)
				return
			}
			if err := updateJobStep(ctx, worker.pool, job.ID, len(businesses), created, updated); err != nil {
				worker.fail(ctx, job.ID, err)
				return
			}
		}
	}
	if err := finishJob(ctx, worker.pool, job.ID, "completed", ""); err != nil {
		log.Printf("complete discovery job %s: %v", job.ID, err)
		return
	}
	completed, err := getJob(ctx, worker.pool, job.ID)
	if err == nil {
		if err := worker.publishCompleted(ctx, completed); err != nil {
			log.Printf("publish discovery completion for %s: %v", job.ID, err)
		}
	}
}

func (worker *DiscoveryWorker) fail(ctx context.Context, jobID string, err error) {
	message := err.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	if finishErr := finishJob(ctx, worker.pool, jobID, "failed", message); finishErr != nil {
		log.Printf("fail discovery job %s: %v (original: %v)", jobID, finishErr, err)
	}
}

func (worker *DiscoveryWorker) publishCompleted(ctx context.Context, job DiscoveryJob) error {
	eventURL := strings.TrimRight(os.Getenv("HUB_EVENT_URL"), "/") + "/events/publish"
	if strings.HasPrefix(eventURL, "/") {
		return errors.New("HUB_EVENT_URL is unavailable")
	}
	completedAt := time.Now().UTC()
	if job.CompletedAt != nil {
		completedAt = *job.CompletedAt
	}
	eventID := newID("evt")
	envelope := map[string]any{
		"specversion": "1.0", "id": eventID, "source": "urn:businesshub:plugin:" + pluginID,
		"type": "com.businesshub.local-businesses.discovery-completed.v1", "subject": job.ID,
		"time": completedAt, "datacontenttype": "application/json",
		"dataschema":    "urn:businesshub:schema:com.businesshub.local-businesses.discovery-completed.v1",
		"correlationid": job.ID,
		"data":          map[string]any{"jobId": job.ID, "name": job.Name, "areaCount": len(job.Areas), "groups": job.Groups, "recordsSeen": job.RecordsSeen, "recordsCreated": job.RecordsCreated, "recordsUpdated": job.RecordsUpdated, "completedAt": completedAt},
	}
	body, _ := json.Marshal(envelope)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, eventURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv("HUB_SERVICE_TOKEN"))
	request.Header.Set("Content-Type", "application/json")
	response, err := worker.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		contents, _ := io.ReadAll(io.LimitReader(response.Body, 1000))
		return fmt.Errorf("event gateway returned %s: %s", response.Status, strings.TrimSpace(string(contents)))
	}
	return nil
}
