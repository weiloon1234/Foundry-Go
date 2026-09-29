// Package httpclient provides named, bounded outbound HTTP clients over reusable
// net/http transports. Clients own operation lifetimes; custom transports are borrowed.
package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
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
	// TLS customizes certificate verification and client certificates for the
	// owned transport. A supplied custom transport cannot combine with it.
	TLS TLSConfig
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
	if _, err := c.TLS.build(); err != nil {
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
// Jitter draws each backoff uniformly from zero to its exponential cap ("full
// jitter") so synchronized clients spread out. Statuses replaces the default
// retryable statuses (408, 429, 500, 502, 503, 504) when nonempty. A
// Retry-After response header on a retryable status sets the wait; a value
// beyond MaxBackoff ends retrying and returns that response.
type RetryPolicy struct {
	Mode                       RetryMode
	Attempts                   int
	InitialBackoff, MaxBackoff time.Duration
	Jitter                     bool
	Statuses                   []int `config:",json"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Mode: SafeReads, Attempts: 3, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 2 * time.Second, Jitter: true}
}
func NoRetries() RetryPolicy { return RetryPolicy{Mode: SafeReads, Attempts: 1} }
func (p RetryPolicy) Validate() error {
	if p.Mode != SafeReads && p.Mode != IdempotentOperation || p.Attempts < 1 || p.Attempts > 10 || p.InitialBackoff < 0 || p.MaxBackoff < p.InitialBackoff || p.MaxBackoff > time.Minute || len(p.Statuses) > 32 {
		return invalid()
	}
	for _, status := range p.Statuses {
		if status < 400 || status > 599 {
			return invalid()
		}
	}
	return nil
}
func (p RetryPolicy) snapshot() RetryPolicy {
	p.Statuses = slices.Clone(p.Statuses)
	return p
}
func (p RetryPolicy) attempts(method string, body Body) int {
	if !body.replayable() || p.Mode == SafeReads && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		return 1
	}
	return p.Attempts
}

// backoff is the exponential cap after completed attempts.
func (p RetryPolicy) backoff(completed int) time.Duration {
	delay := p.InitialBackoff
	for i := 1; i < completed && delay < p.MaxBackoff; i++ {
		delay = min(delay*2, p.MaxBackoff)
	}
	return delay
}
func (p RetryPolicy) delay(completed int) time.Duration {
	delay := p.backoff(completed)
	if p.Jitter && delay > 0 {
		return time.Duration(rand.Int64N(int64(delay) + 1))
	}
	return delay
}
func (p RetryPolicy) retryStatus(status int) bool {
	if len(p.Statuses) > 0 {
		return slices.Contains(p.Statuses, status)
	}
	switch status {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// retryAfter parses a Retry-After header as delay-seconds or an HTTP date.
func retryAfter(headers http.Header, now time.Time) (time.Duration, bool) {
	text := strings.TrimSpace(headers.Get("Retry-After"))
	if text == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(text, 10, 64); err == nil {
		if seconds < 0 || seconds > int64(time.Hour/time.Second) {
			return time.Hour, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	at, err := http.ParseTime(text)
	if err != nil {
		return 0, false
	}
	return max(0, at.Sub(now)), true
}

// TLSConfig customizes the owned transport's TLS. CertificateAuthorities
// (PEM) replaces the system roots unless AppendSystemRoots is set.
// Certificate and PrivateKey (PEM) present a client certificate and must be
// set together. ServerName overrides verification of the URL host name.
type TLSConfig struct {
	CertificateAuthorities string
	AppendSystemRoots      bool
	Certificate            secret.String
	PrivateKey             secret.String
	ServerName             string
}

func (t TLSConfig) IsZero() bool {
	return t.CertificateAuthorities == "" && !t.AppendSystemRoots && t.Certificate.IsZero() && t.PrivateKey.IsZero() && t.ServerName == ""
}

// build parses the PEM material. A zero configuration returns nil.
func (t TLSConfig) build() (*tls.Config, error) {
	if t.IsZero() {
		return nil, nil
	}
	if len(t.CertificateAuthorities) > 1<<20 || len(t.ServerName) > 253 || t.AppendSystemRoots && t.CertificateAuthorities == "" || t.Certificate.IsZero() != t.PrivateKey.IsZero() {
		return nil, invalid()
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: t.ServerName}
	if t.CertificateAuthorities != "" {
		pool := x509.NewCertPool()
		if t.AppendSystemRoots {
			system, err := x509.SystemCertPool()
			if err != nil {
				return nil, fault.Wrap(fault.Invalid, "system certificate roots are unavailable", err)
			}
			pool = system
		}
		if !pool.AppendCertsFromPEM([]byte(t.CertificateAuthorities)) {
			return nil, invalid()
		}
		config.RootCAs = pool
	}
	if !t.Certificate.IsZero() {
		certificate, err := tls.X509KeyPair([]byte(t.Certificate.Reveal()), []byte(t.PrivateKey.Reveal()))
		if err != nil {
			return nil, fault.New(fault.Invalid, "invalid outbound HTTP client certificate")
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}
