// Package httpclient provides named, bounded outbound HTTP clients over reusable
// net/http transports. Clients own operation lifetimes; custom transports are borrowed.
package httpclient

import (
	"fmt"
	"net/http"
	"time"
)

type Name string

const MaxBodyBytes int64 = 64 << 20

type Config struct {
	// Destination optionally enforces direct, address-checked outbound networking.
	Destination    DestinationPolicy
	Name           Name
	BaseURL        string
	Headers        http.Header `config:",json,secret"`
	ConnectTimeout time.Duration
	AttemptTimeout time.Duration
	Timeout        time.Duration
	Concurrency    int
	RequestBytes   int64
	ResponseBytes  int64
	HeaderBytes    int
	Retry          RetryPolicy
	// PropagateTrace opts this named destination into bounded W3C correlation.
	// When enabled, framework context replaces manual trace headers on the
	// owned request copy. Disabled clients do not add propagation headers.
	PropagateTrace bool
}

func DefaultConfig(name Name) Config {
	return Config{Name: name, ConnectTimeout: 10 * time.Second, AttemptTimeout: 30 * time.Second, Timeout: time.Minute, Concurrency: 64, RequestBytes: 4 << 20, ResponseBytes: 4 << 20, HeaderBytes: 64 << 10, Retry: DefaultRetryPolicy()}
}
func (c Config) Validate() error {
	if c.Name.Validate() != nil || c.ConnectTimeout <= 0 || c.ConnectTimeout > time.Hour || c.AttemptTimeout <= 0 || c.AttemptTimeout > time.Hour || c.Timeout <= 0 || c.Timeout > time.Hour || c.Concurrency < 1 || c.Concurrency > 4096 || c.RequestBytes < 1 || c.RequestBytes > MaxBodyBytes || c.ResponseBytes < 1 || c.ResponseBytes > MaxBodyBytes || c.HeaderBytes < 1 || c.HeaderBytes > 1<<20 {
		return invalid()
	}
	if c.BaseURL != "" {
		if _, err := baseURL(c.BaseURL); err != nil {
			return err
		}
	}
	if _, err := copyHeaders(c.Headers, c.HeaderBytes, true); err != nil {
		return err
	}
	if err := c.Destination.Validate(); err != nil {
		return err
	}
	return c.Retry.Validate()
}
func (Config) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("HTTP client configuration"))
}
func (Config) MarshalJSON() ([]byte, error) { return []byte(`"HTTP client configuration"`), nil }

type RetryMode string

const (
	SafeReads           RetryMode = "safe_reads"
	IdempotentOperation RetryMode = "idempotent_operation"
)

// RetryPolicy always counts total attempts, including the first. Mutation retries
// require an explicit IdempotentOperation policy and a replayable request body.
type RetryPolicy struct {
	Mode                       RetryMode
	Attempts                   int
	InitialBackoff, MaxBackoff time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Mode: SafeReads, Attempts: 3, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 2 * time.Second}
}
func NoRetries() RetryPolicy { return RetryPolicy{Mode: SafeReads, Attempts: 1} }
func (p RetryPolicy) Validate() error {
	if p.Mode != SafeReads && p.Mode != IdempotentOperation || p.Attempts < 1 || p.Attempts > 10 || p.InitialBackoff < 0 || p.MaxBackoff < p.InitialBackoff || p.MaxBackoff > time.Minute {
		return invalid()
	}
	return nil
}
func (p RetryPolicy) attempts(method string, body Body) int {
	if !body.replayable() || p.Mode == SafeReads && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		return 1
	}
	return p.Attempts
}
func (p RetryPolicy) delay(completed int) time.Duration {
	delay := p.InitialBackoff
	for i := 1; i < completed && delay < p.MaxBackoff; i++ {
		delay = min(delay*2, p.MaxBackoff)
	}
	return delay
}
func retryStatus(status int) bool {
	switch status {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}
