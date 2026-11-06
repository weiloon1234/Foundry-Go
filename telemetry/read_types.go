package telemetry

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Window selects a bounded number of minutes. Applications may offer a narrower list.
type Window struct{ Minutes int }

// Validate rejects windows outside one minute through the maximum retention.
func (w Window) Validate() error {
	if w.Minutes < 1 || w.Minutes > int(maxRetention/time.Minute) {
		return invalid("invalid telemetry window")
	}
	return nil
}

// OverviewQuery selects the UI window; retained runs and incidents remain visible independently of it.
type OverviewQuery struct{ Window Window }

// TrafficQuery selects aggregated traffic, optionally including excluded routes.
type TrafficQuery struct {
	Window          Window
	IncludeExcluded bool
}

// IssueQuery selects one source and a strictly bounded page.
type IssueQuery struct {
	Source Source
	Page   orm.PageRequest
}

// EventQuery selects retained samples for a canonical issue fingerprint.
type EventQuery struct {
	Fingerprint Fingerprint
	Page        orm.PageRequest
}

// AlertQuery optionally filters incident state and selects a bounded page.
type AlertQuery struct {
	State value.Optional[AlertState]
	Page  orm.PageRequest
}

// Acknowledgement identifies the incident episode acknowledged in the caller's transaction.
type Acknowledgement struct {
	Key     AlertKey
	Episode Episode
}

// Issue is an owned retained issue-group snapshot.
type Issue struct {
	Fingerprint     Fingerprint
	Source          Source
	Code            string
	Route           http.RouteID
	Release         Release
	Count           int64
	FirstAt, LastAt temporal.DateTime
	Detail          Detail
}

// Event is an owned retained sample and the owner of public event UUIDs.
type Event struct {
	ID        model.ID[Event]
	CreatedAt temporal.DateTime
	Detail    Detail
}

// Alert is an incident snapshot without application actor attribution.
type Alert struct {
	Key             AlertKey
	RunID           model.ID[Run]
	Episode         Episode
	Rule            Rule
	State           AlertState
	FirstAt, LastAt temporal.DateTime
	RecoveredAt     value.Nullable[temporal.DateTime]
}

// TrafficRow aggregates compatible cumulative histograms across retained runs and buckets.
type TrafficRow struct {
	Route         http.RouteID
	RoutePath     string
	Method        http.Method
	Status        int
	Outcome       observability.Outcome
	Count         uint64
	Seconds       float64
	P50, P95, P99 observability.QuantileEstimate
}

// TrafficSummary describes the included groups; Failed counts HTTP statuses at least 500.
type TrafficSummary struct {
	Count, Failed uint64
	P95           observability.QuantileEstimate
}

// Traffic contains owned rows. Truncated means a row or group limit omitted observations.
type Traffic struct {
	Items     []TrafficRow
	Summary   TrafficSummary
	Truncated bool
}

// Overview contains owned retained snapshots. Truncated signals either list's limit.
type Overview struct {
	Runs         []Run
	Alerts       []Alert
	OldestBucket value.Optional[temporal.DateTime]
	Truncated    bool
}
