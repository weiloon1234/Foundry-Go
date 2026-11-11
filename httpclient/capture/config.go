// Package capture stores bounded, best-effort diagnostics for every attempt an
// application's outbound HTTP clients make, and serves guarded viewer routes.
// Capture is on by default inside an application with a database. Deployment
// settings can only reduce it (a deny-list, forced redaction, extra masked
// names, off); exact bodies need application.Builder.FullOutboundCapture.
package capture

import (
	"strings"
	"time"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/capturedisclosure"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/httptoken"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const (
	// MaxExclusions bounds the deny-list.
	MaxExclusions = 64
	// MaxMaskNames bounds the extra credential names.
	MaxMaskNames = 64
)

// Config holds deployment policy and limits for outbound capture. Settings
// load it as features.outbound_capture.config.
//
// Example: config := capture.DefaultConfig(); config.Exclude = []capture.Exclusion{{Client: "crawler"}}
type Config struct {
	// BodyBytes bounds each stored body (1 KiB to 8 MiB). A larger body is
	// omitted in redacted mode and stored as an exact prefix in full mode.
	BodyBytes int
	// ValueBytes bounds a redacted string or field value (64 bytes to 1 MiB);
	// longer values read "[omitted N bytes]".
	ValueBytes int
	// ForceRedacted stores every client redacted, overriding
	// Builder.FullOutboundCapture.
	ForceRedacted bool
	// MaskNames adds credential names for headers, query values and body
	// fields, matched exactly and case-insensitively (at most 64, each 1 to
	// 128 bytes).
	MaskNames []string `config:",json"`
	// Exclude is the deny-list (at most 64 rules). A call matching any rule
	// is not captured.
	Exclude []Exclusion `config:",json"`
	// Rate admits at most this many captures per second per client per
	// process (1 to 10,000). Refused calls are made normally and counted as lost.
	Rate int
	// Burst is the per-client admission burst (1 to 10,000).
	Burst int
	// QueueRows bounds captures in flight, queued or being written per
	// process (1 to 256). One client may hold at most a quarter.
	QueueRows int
	// RetainedBytes bounds memory those captures hold per process (up to
	// 1 GiB). One client may hold at most a quarter, which must fit one body.
	RetainedBytes int64
	// Retention deletes captures older than this (1 hour to 365 days).
	Retention time.Duration
	// MaxRows keeps about this many newest captures per client (1,000 to
	// 10,000,000). Pruning catches up every minute while over the cap.
	MaxRows int
}

// DefaultConfig returns 1 MiB bodies, 4 KiB values, 20 captures per second
// per client with a burst of 40, 256 captures within 64 MiB per process,
// 30-day retention and 20,000 rows per client, with no exclusions.
func DefaultConfig() Config {
	return Config{BodyBytes: 1 << 20, ValueBytes: 4 << 10, Rate: 20, Burst: 40, QueueRows: 256, RetainedBytes: 64 << 20, Retention: 30 * 24 * time.Hour, MaxRows: 20_000}
}

// Validate checks every limit, extra name and rule at the settings boundary.
func (c Config) Validate() error {
	if err := c.recorder().Validate(); err != nil {
		return invalid("outbound capture admission limits are out of range")
	}
	if c.ValueBytes < 64 || c.ValueBytes > 1<<20 || c.Retention < time.Hour || c.Retention > 365*24*time.Hour || c.MaxRows < 1_000 || c.MaxRows > 10_000_000 {
		return invalid("outbound capture limits are out of range")
	}
	if len(c.MaskNames) > MaxMaskNames {
		return invalid("outbound capture has too many mask names")
	}
	for _, name := range c.MaskNames {
		if name == "" || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
			return invalid("outbound capture has an invalid mask name")
		}
	}
	_, err := rules(c.Exclude)
	return err
}

func (c Config) recorder() httpcapture.Config {
	return httpcapture.Config{BodyBytes: c.BodyBytes, RetainedBytes: c.RetainedBytes, QueueSize: c.QueueRows, Rate: c.Rate, Burst: c.Burst, LaneShare: httpcapture.OutboundLaneShare}
}

func (c Config) policy() capturedisclosure.Policy {
	return capturedisclosure.Policy{ValueBytes: c.ValueBytes, BodyBytes: c.BodyBytes, Names: capturedisclosure.NewNames(c.MaskNames)}
}

// Exclusion is one deny-list rule. A rule matches when every field it sets
// matches; an empty rule is invalid. Rules see only request facts, never the
// response.
type Exclusion struct {
	// Client matches the client's configured name exactly. A rule with only a
	// client excludes that client entirely.
	Client httpclient.Name `json:"client,omitzero"`
	// Host matches the URL host without port, case-insensitively:
	// "api.example.com", or "*.example.com" for any subdomain (the apex needs
	// its own rule).
	Host string `json:"host,omitzero"`
	// Method matches the upper-case request method.
	Method string `json:"method,omitzero"`
	// PathPrefix matches the decoded path segment by segment; it is validated
	// like http.ParsePathPrefix.
	PathPrefix string `json:"path_prefix,omitzero"`
}

func rules(exclusions []Exclusion) ([]httpcapture.Rule, error) {
	if len(exclusions) > MaxExclusions {
		return nil, invalid("outbound capture has too many exclusions")
	}
	result := make([]httpcapture.Rule, 0, len(exclusions))
	seen := make(map[httpcapture.Rule]bool, len(exclusions))
	for _, exclusion := range exclusions {
		rule := httpcapture.Rule{Client: string(exclusion.Client), Host: strings.ToLower(exclusion.Host), Method: exclusion.Method}
		if rule.Client != "" && exclusion.Client.Validate() != nil {
			return nil, invalid("outbound capture exclusion has an invalid client")
		}
		if rule.Host != "" && !validHost(rule.Host) {
			return nil, invalid("outbound capture exclusion host must be a host name without scheme, port or path")
		}
		if rule.Method != "" && (len(rule.Method) > 32 || !httptoken.Valid(rule.Method) || rule.Method != strings.ToUpper(rule.Method)) {
			return nil, invalid("outbound capture exclusion has an invalid method")
		}
		if exclusion.PathPrefix != "" {
			prefix, err := http.ParsePathPrefix(exclusion.PathPrefix)
			if err != nil {
				return nil, invalid("outbound capture exclusion has an invalid path prefix")
			}
			rule.PathPrefix = strings.TrimSuffix(prefix.String(), "/")
		}
		// A rule matching every call is the enabled switch, not an exclusion.
		if rule == (httpcapture.Rule{}) {
			return nil, invalid("outbound capture exclusion is empty")
		}
		if seen[rule] {
			return nil, invalid("outbound capture has a duplicate exclusion")
		}
		seen[rule] = true
		result = append(result, rule)
	}
	return result, nil
}

// validHost accepts a lowercase DNS name or IPv4 address, optionally as
// "*.suffix"; a bare "*", a scheme, port, path or bracketed address is not.
func validHost(host string) bool {
	host = strings.TrimPrefix(host, "*.")
	if host == "" || len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	return !strings.ContainsFunc(host, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '.' })
}

// NewRecorder validates config and builds the static, I/O-free recorder that
// application assembly contributes to every httpclient.Module. full lists the
// Go full-body declarations; config.ForceRedacted empties it.
func NewRecorder(config Config, full []httpclient.Name) (*httpcapture.Outbound, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	exclusions, err := rules(config.Exclude)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, name := range full {
		if name.Validate() != nil {
			return nil, invalid("outbound capture full-body declaration names an invalid client")
		}
		if !config.ForceRedacted {
			names = append(names, string(name))
		}
	}
	return httpcapture.NewOutbound(httpcapture.OutboundConfig{Limits: config.recorder(), Full: names, Rules: exclusions, Inspect: inspectable})
}

// inspectable is the tap's before-buffering decision. Redaction calls the same
// disclosure function again, so the two never disagree.
func inspectable(contentType, contentEncoding string) httpcapture.Skip {
	switch _, reason := capturedisclosure.Inspectable(contentType, contentEncoding); reason {
	case capturedisclosure.NoReason:
		return httpcapture.Kept
	case capturedisclosure.ContentEncoded:
		return httpcapture.SkipEncoded
	default:
		return httpcapture.SkipMedia
	}
}

func invalid(message string) error { return safeerror.NewFault(fault.Invalid, message) }
