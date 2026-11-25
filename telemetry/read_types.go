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
type Window struct {
	// Minutes is the window length in minutes. It must be at least 1 and at most the longest retention
	// (thirty days). A traffic read above 60 minutes uses hourly buckets.
	Minutes int
}

// Validate rejects windows outside one minute through the maximum retention.
func (w Window) Validate() error {
	if w.Minutes < 1 || w.Minutes > int(maxRetention/time.Minute) {
		return invalid("invalid telemetry window")
	}
	return nil
}

// OverviewQuery selects the UI window; retained runs and incidents remain visible independently of it.
type OverviewQuery struct {
	// Window is validated, but it does not change which runs, incidents or buckets Overview returns.
	Window Window
}

// TrafficQuery selects aggregated traffic, optionally including excluded routes.
type TrafficQuery struct {
	// Window is the span of traffic to aggregate, ending now and limited by the retained buckets.
	Window Window
	// IncludeExcluded includes routes excluded by the collector's RoutePolicy. Without it they are
	// skipped.
	IncludeExcluded bool
}

// IssueQuery selects one source and a strictly bounded page.
type IssueQuery struct {
	// Source selects the issues of one source. It must be a known Source.
	Source Source
	// Page selects the page. It must be valid and within the configured ReadLimits.MaxPage and
	// MaxPageSize.
	Page orm.PageRequest
}

// EventQuery selects retained samples for a canonical issue fingerprint.
type EventQuery struct {
	// Fingerprint identifies the issue group whose events are read. It must be a canonical lowercase
	// SHA-256 identity.
	Fingerprint Fingerprint
	// Page selects the page. It must be valid and within the configured ReadLimits.MaxPage and
	// MaxPageSize.
	Page orm.PageRequest
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
