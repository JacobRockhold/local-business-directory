package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireMutationChecksManagedIdentityAndCSRF(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/discovery-jobs", nil)
	request.Header.Set("X-FileForge-User-ID", "usr_test")
	request.Header.Set("X-CSRF-Token", "token-value")
	request.AddCookie(&http.Cookie{Name: "hub_csrf", Value: "token-value"})
	response := httptest.NewRecorder()
	if !requireMutation(response, request) {
		t.Fatalf("expected matching managed request to pass: %s", response.Body.String())
	}

	bad := httptest.NewRequest("POST", "/api/discovery-jobs", nil)
	bad.Header.Set("X-FileForge-User-ID", "usr_test")
	bad.Header.Set("X-CSRF-Token", "wrong")
	bad.AddCookie(&http.Cookie{Name: "hub_csrf", Value: "token-value"})
	badResponse := httptest.NewRecorder()
	if requireMutation(badResponse, bad) || badResponse.Code != 403 {
		t.Fatalf("expected mismatched CSRF to fail, received %d", badResponse.Code)
	}
}

func TestBackfillCursorRoundTrip(t *testing.T) {
	want := backfillCursor{SnapshotTime: time.Now().UTC().Truncate(time.Microsecond), UpdatedAt: time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond), ID: "osm:node:123"}
	got, err := decodeBackfillCursor(encodeBackfillCursor(want))
	if err != nil || !got.SnapshotTime.Equal(want.SnapshotTime) || !got.UpdatedAt.Equal(want.UpdatedAt) || got.ID != want.ID {
		t.Fatalf("cursor did not round-trip: got %#v, err %v", got, err)
	}
}
