package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildOverpassQueryUsesBroadNamedGroup(t *testing.T) {
	query, err := buildOverpassQuery(Area{Label: "Test", Latitude: 32.7767, Longitude: -96.7970, RadiusMeters: 15000}, "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`nwr(around:15000,32.776700,-96.797000)`, `["name"]["shop"]`, "out center tags qt"} {
		if !strings.Contains(query, expected) {
			t.Fatalf("query does not contain %q: %s", expected, query)
		}
	}
}

func TestTransformElementsNormalizesAndDeduplicates(t *testing.T) {
	latitude, longitude := 32.77, -96.79
	elements := []overpassElement{
		{Type: "node", ID: 42, Latitude: &latitude, Longitude: &longitude, Tags: map[string]string{"name": "Example Bakery", "shop": "bakery", "addr:housenumber": "10", "addr:street": "Main St", "contact:phone": "+1 555 0100"}},
		{Type: "node", ID: 42, Latitude: &latitude, Longitude: &longitude, Tags: map[string]string{"name": "Example Bakery", "shop": "bakery", "addr:city": "Dallas"}},
		{Type: "way", ID: 99, Tags: map[string]string{"name": "Missing coordinates", "shop": "yes"}},
	}
	businesses := transformElements(elements, "shop")
	if len(businesses) != 1 {
		t.Fatalf("expected one deduplicated business, received %d", len(businesses))
	}
	if businesses[0].ID != "osm:node:42" || businesses[0].PrimaryCategory != "shop:bakery" || businesses[0].City != "Dallas" {
		t.Fatalf("unexpected normalized business: %#v", businesses[0])
	}
}

func TestValidateEndpointBlocksInternalTargets(t *testing.T) {
	for _, endpoint := range []string{"http://overpass-api.de/api/interpreter", "https://localhost/api", "https://127.0.0.1/api", "https://service.local/api"} {
		if validateEndpoint(endpoint) == nil {
			t.Fatalf("expected endpoint to be rejected: %s", endpoint)
		}
	}
	if err := validateEndpoint("https://overpass-api.de/api/interpreter"); err != nil {
		t.Fatalf("expected public HTTPS endpoint to pass: %v", err)
	}
}

func TestValidateGroupsDefaultsAndDeduplicates(t *testing.T) {
	defaults, err := validateGroups(nil)
	if err != nil || len(defaults) != len(defaultDiscoveryGroups) {
		t.Fatalf("unexpected defaults: %v %v", defaults, err)
	}
	groups, err := validateGroups([]string{"shop", " SHOP ", "office"})
	if err != nil || len(groups) != 2 || groups[0] != "shop" || groups[1] != "office" {
		t.Fatalf("unexpected normalized groups: %v %v", groups, err)
	}
}

func TestOverpassLive(t *testing.T) {
	if os.Getenv("OVERPASS_INTEGRATION") != "1" {
		t.Skip("set OVERPASS_INTEGRATION=1 to run the courteous one-request source check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := NewOverpassClient().Fetch(ctx, "https://overpass-api.de/api/interpreter", Area{Label: "Dallas verification", Latitude: 32.7767, Longitude: -96.7970, RadiusMeters: 250}, "shop")
	if err != nil {
		t.Fatalf("live Overpass-compatible request failed: %v", err)
	}
}
