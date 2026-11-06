package telemetry

import (
	"net/netip"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/health"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Instance identifies a configured process instance, never an end user.
type Instance string

// Release identifies the deployed build accepting observations.
type Release string

// Portal identifies an application-declared browser portal.
type Portal string

// Source identifies the origin of an issue observation.
//
//foundry:enum
type Source string

const (
	SourceBackend  Source = "backend"
	SourceBrowser  Source = "browser"
	SourceSecurity Source = "security"
)

// Rule identifies a built-in incident rule; values do not enable evaluation.
//
//foundry:enum
type Rule string

const (
	RuleLatency             Rule = "latency"
	RuleHTTPErrors          Rule = "http_errors"
	RulePoolPressure        Rule = "pool_pressure"
	RuleLogFailures         Rule = "log_failures"
	RuleTelemetryLoss       Rule = "telemetry_loss"
	RuleAuthenticationBurst Rule = "authentication_burst"
)

// AlertState identifies an incident's state.
//
//foundry:enum
type AlertState string

const (
	AlertStateOpen         AlertState = "open"
	AlertStateAcknowledged AlertState = "acknowledged"
	AlertStateRecovered    AlertState = "recovered"
)

// Fingerprint is a lowercase SHA-256 issue identity. Parse external values with
// ParseFingerprint; conversion from string alone does not validate it.
type Fingerprint string

// AlertKey is a lowercase SHA-256 incident identity. Parse external values with
// ParseAlertKey; conversion from string alone does not validate it.
type AlertKey string

// Episode identifies a reopening of an incident.
type Episode uint64

// ParseFingerprint validates a canonical lowercase SHA-256 identity.
func ParseFingerprint(text string) (Fingerprint, error) {
	if !validDigest(text) {
		return "", invalid("invalid telemetry fingerprint")
	}
	return Fingerprint(text), nil
}

// ParseAlertKey validates a canonical lowercase SHA-256 identity.
func ParseAlertKey(text string) (AlertKey, error) {
	if !validDigest(text) {
		return "", invalid("invalid telemetry alert key")
	}
	return AlertKey(text), nil
}

// Run is an owned process observation snapshot and the owner of run UUIDs.
type Run struct {
	ID                     model.ID[Run]
	Instance               Instance
	Release                Release
	StartedAt, HeartbeatAt temporal.DateTime
	Stale                  bool
	Runtime                Runtime
}

// Runtime contains bounded samples and cumulative best-effort loss counters.
// Pending counts older retained batches at capture time. StorageFailures remains
// separate from admission loss; collection alone performs no storage operations.
type Runtime struct {
	Health                   Health
	Samples                  []Sample
	Dropped, StorageFailures uint64
	Pending                  int
}

// Health contains structural health only. An unset Ready means unavailable.
type Health struct {
	Ready        value.Nullable[bool]
	State        foundation.State
	Mode         maintenance.Mode
	Dependencies []DependencyHealth
}

// DependencyHealth retains a probe identity and state, never dependency errors.
type DependencyHealth struct {
	ID    health.ProbeID
	State health.State
}

// Sample is a recorder-owned metric copied at capture. Labels is the escaped
// Prometheus label representation, not markup or a payload channel.
type Sample struct {
	Name, Labels string
	Value        float64
}

// Frame is a bounded source location without arguments, locals or error text.
type Frame struct {
	File, Function string
	Line, Column   int
}

// Detail contains redacted issue evidence. ClientIP is protected detail, never a
// metric label; callers must authorize access before exposing it.
type Detail struct {
	Frames    []Frame
	RequestID attribution.RequestID
	TraceID   tracing.TraceID
	Path      string
	ClientIP  value.Optional[netip.Addr] `json:",omitzero"`
	Untrusted bool
}
