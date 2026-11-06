// Package store owns telemetry persistence declarations. Type parameters retain
// the public package's concrete identities and payloads without an import cycle.
// These models are private infrastructure, not transport DTOs.
package store

import (
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TextEnum preserves the framework enum descriptor at the persistence boundary.
type TextEnum[E ~string] interface {
	~string
	EnumDescriptor() enum.Descriptor[E]
}

// MetricKey is a SHA-256 identity for one run/interval/route metric bucket.
type MetricKey string

// Envelope versions persisted JSON independently of row schema migrations.
type Envelope[T any] struct {
	Version int `json:"version"`
	Data    T   `json:"data"`
}

// Histogram stores the existing framework's cumulative duration buckets.
type Histogram struct {
	Count   uint64   `json:"count"`
	Seconds float64  `json:"seconds"`
	Buckets []uint64 `json:"buckets"`
}

// Control is the typed row for foundry_telemetry_controls.
type Control struct {
	Key string
}

// Run is the typed row for foundry_telemetry_runs.
type Run[R any, I, L ~string] struct {
	ID          model.ID[Run[R, I, L]]
	Instance    I
	Release     L
	StartedAt   temporal.DateTime
	HeartbeatAt temporal.DateTime
	RetainUntil temporal.DateTime
	Runtime     value.JSON[Envelope[R]]
}

// Flush is the typed row for foundry_telemetry_flushes.
type Flush[O, P any] struct {
	ID          model.ID[Flush[O, P]]
	RunID       model.ID[O]
	CreatedAt   temporal.DateTime
	StoredAt    temporal.DateTime `foundry:"default=database"`
	RetainUntil temporal.DateTime
	Result      value.JSON[Envelope[P]]
}

// Metric is the typed row for foundry_telemetry_metric_buckets.
type Metric[O any] struct {
	Key             MetricKey
	RunID           model.ID[O]
	BucketAt        temporal.DateTime
	IntervalSeconds int
	Route           http.RouteID
	RoutePath       string
	Method          http.Method
	Status          int
	Outcome         observability.Outcome
	Histogram       value.JSON[Envelope[Histogram]]
}

// Issue is the typed row for foundry_telemetry_issues.
type Issue[D any, S ~string, F, L ~string] struct {
	Fingerprint F
	Source      S
	Code        string
	Route       http.RouteID
	Release     L
	Count       int64
	FirstAt     temporal.DateTime
	LastAt      temporal.DateTime
	Detail      value.JSON[Envelope[D]]
}

// Event is the typed row for foundry_telemetry_events.
type Event[D any, S ~string, F ~string] struct {
	ID          model.ID[Event[D, S, F]]
	Fingerprint F
	Source      S
	CreatedAt   temporal.DateTime
	Detail      value.JSON[Envelope[D]]
}

// Alert is the typed row for foundry_telemetry_alerts.
type Alert[O any, Q ~string, S ~string, K ~string, E ~uint64] struct {
	Key         K
	RunID       model.ID[O]
	Rule        Q
	State       S
	Episode     E
	FirstAt     temporal.DateTime
	LastAt      temporal.DateTime
	RecoveredAt value.Nullable[temporal.DateTime]
}
