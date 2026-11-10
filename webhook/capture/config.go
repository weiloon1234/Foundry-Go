// Package capture stores bounded, best-effort diagnostics for requests under
// declared webhook prefixes and serves guarded viewer routes. Prefixes and
// full-body mode are Go declarations (application.Builder.CaptureWebhooks);
// deployment settings can only reduce capture, never extend it.
package capture

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Config holds deployment limits for one capture store. Settings load it as
// features.webhook_capture.config.
//
// Example: config := capture.DefaultConfig(); config.Retention = 7 * 24 * time.Hour
type Config struct {
	// BodyBytes bounds the request body read before the handler (1 KiB to
	// 8 MiB). A larger body is omitted; the handler still receives every byte.
	BodyBytes int
	// ValueBytes bounds a redacted JSON string or form, query or multipart
	// field value (64 bytes to 1 MiB); longer values read "[omitted N bytes]".
	ValueBytes int
	// ForceRedacted stores every prefix redacted, including prefixes declared
	// with http.FullCaptureBody.
	ForceRedacted bool
	// Rate admits at most this many captures per second per process (1 to
	// 10,000). Refused requests are served normally and counted as lost.
	Rate int
	// Burst is the admission burst (1 to 10,000).
	Burst int
	// QueueRows bounds captures being read, queued or written (1 to 256).
	QueueRows int
	// RetainedBytes bounds memory held by those captures (enough for one
	// capture, up to 1 GiB).
	RetainedBytes int64
	// Retention deletes captures older than this (1 hour to 365 days).
	Retention time.Duration
	// MaxRows keeps about this many newest captures in the table (1,000 to
	// 10,000,000). Pruning catches up every minute while over the cap.
	MaxRows int
}

// DefaultConfig returns a 2 MiB body limit, 4 KiB values, 50 captures per
// second with a burst of 50, 256 queued captures within 64 MiB, 30-day
// retention and 100,000 rows.
func DefaultConfig() Config {
	r := httpcapture.DefaultConfig()
	return Config{BodyBytes: r.BodyBytes, ValueBytes: 4 << 10, Rate: r.Rate, Burst: r.Burst, QueueRows: r.QueueSize, RetainedBytes: r.RetainedBytes, Retention: 30 * 24 * time.Hour, MaxRows: 100_000}
}

// Validate checks every limit against its hard cap.
func (c Config) Validate() error {
	if err := c.recorder().Validate(); err != nil {
		return invalid("webhook capture admission limits are out of range")
	}
	if c.ValueBytes < 64 || c.ValueBytes > 1<<20 || c.Retention < time.Hour || c.Retention > 365*24*time.Hour || c.MaxRows < 1_000 || c.MaxRows > 10_000_000 {
		return invalid("webhook capture limits are out of range")
	}
	return nil
}

func (c Config) recorder() httpcapture.Config {
	return httpcapture.Config{BodyBytes: c.BodyBytes, RetainedBytes: c.RetainedBytes, QueueSize: c.QueueRows, Rate: c.Rate, Burst: c.Burst}
}

// NewRecorder validates config and builds the static in-memory handoff that
// the HTTP kernel fills and the Store drains. It performs no I/O and starts no
// goroutine. Application assembly passes it to http.WithWebhookCapture and New.
func NewRecorder(config Config) (*httpcapture.Recorder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return httpcapture.New(config.recorder())
}

func invalid(message string) error { return safeerror.NewFault(fault.Invalid, message) }
