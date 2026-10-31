package email

import (
	"fmt"
	"net/http"
	"time"
)

// HTTPConfig configures API adapters. Empty Endpoint selects the provider's
// documented URL; overrides must use HTTPS or an explicit loopback HTTP URL.
// A supplied RoundTripper is borrowed and must not retry submissions itself.
type HTTPConfig struct {
	Endpoint  string
	Timeout   time.Duration
	Transport http.RoundTripper
}

// DefaultHTTPConfig returns HTTP settings with the default mailer timeout.
func DefaultHTTPConfig() HTTPConfig { return HTTPConfig{Timeout: DefaultConfig().Timeout} }

// Format writes a fixed label without exposing HTTP configuration.
func (HTTPConfig) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email HTTP configuration")) }
