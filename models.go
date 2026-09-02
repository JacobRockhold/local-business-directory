package main

import "time"

type Area struct {
	Label        string  `json:"label"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RadiusMeters int     `json:"radiusMeters"`
}

type DiscoveryJob struct {
	ID                string     `json:"id"`
	RequestID         string     `json:"requestId,omitempty"`
	RequestedByPlugin string     `json:"requestedByPlugin,omitempty"`
	Name              string     `json:"name"`
	Status            string     `json:"status"`
	Areas             []Area     `json:"areas"`
	Groups            []string   `json:"groups"`
	TotalSteps        int        `json:"totalSteps"`
	CompletedSteps    int        `json:"completedSteps"`
	RecordsSeen       int        `json:"recordsSeen"`
	RecordsCreated    int        `json:"recordsCreated"`
	RecordsUpdated    int        `json:"recordsUpdated"`
	LastError         string     `json:"lastError,omitempty"`
	ErrorCode         string     `json:"errorCode,omitempty"`
	CreatedBy         string     `json:"createdBy,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
}

type Business struct {
	ID              string            `json:"id"`
	Source          string            `json:"source"`
	SourceType      string            `json:"sourceType"`
	SourceID        string            `json:"sourceId"`
	SourceURL       string            `json:"sourceUrl"`
	Name            string            `json:"name"`
	Categories      []string          `json:"categories"`
	PrimaryCategory string            `json:"primaryCategory"`
	Latitude        float64           `json:"latitude"`
	Longitude       float64           `json:"longitude"`
	Street          string            `json:"street,omitempty"`
	City            string            `json:"city,omitempty"`
	Region          string            `json:"region,omitempty"`
	PostalCode      string            `json:"postalCode,omitempty"`
	Country         string            `json:"country,omitempty"`
	Phone           string            `json:"phone,omitempty"`
	Email           string            `json:"email,omitempty"`
	Website         string            `json:"website,omitempty"`
	OpeningHours    string            `json:"openingHours,omitempty"`
	Tags            map[string]string `json:"tags"`
	Version         int64             `json:"version"`
	FirstSeenAt     time.Time         `json:"firstSeenAt"`
	LastSeenAt      time.Time         `json:"lastSeenAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	DiscoveryJobID  string            `json:"discoveryJobId,omitempty"`
	License         string            `json:"license"`
}

type DirectorySettings struct {
	OverpassEndpoint  string `json:"overpassEndpoint"`
	MinIntervalSecond int    `json:"minIntervalSeconds"`
}

type SearchRequest struct {
	Query          string   `json:"query"`
	Categories     []string `json:"categories"`
	City           string   `json:"city"`
	Region         string   `json:"region"`
	Country        string   `json:"country"`
	DiscoveryJobID string   `json:"discoveryJobId"`
	Limit          int      `json:"limit"`
	Cursor         string   `json:"cursor"`
}

type BackfillRequest struct {
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

type DiscoveryRequestInput struct {
	RequestID string   `json:"requestId"`
	Name      string   `json:"name"`
	Areas     []Area   `json:"areas"`
	Groups    []string `json:"groups"`
}

type DiscoveryStatusRequest struct {
	RequestID string `json:"requestId"`
}

type DiscoveryRequestRecord struct {
	CallerPlugin string    `json:"callerPlugin"`
	RequestID    string    `json:"requestId"`
	PayloadHash  string    `json:"payloadHash"`
	JobID        string    `json:"jobId"`
	CreatedAt    time.Time `json:"createdAt"`
}
