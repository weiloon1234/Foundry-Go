// Package http provides Foundry's HTTP transport and application kernel while
// retaining interoperability with standard net/http handlers.
package http

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ServerConfig bounds native HTTP connection I/O. Start with DefaultServerConfig
// and change fields explicitly. All timeouts and the header limit must be
// positive. MaxBodyBytes is a positive global ceiling, including streamed bodies.
// I/O deadlines do not terminate arbitrary handler computations.
type ServerConfig struct {
	// Address is a TCP host:port. Port zero allocates a port reported by Ready.
	Address string
	// ReadHeaderTimeout bounds socket time spent reading request headers.
	ReadHeaderTimeout time.Duration
	// ReadTimeout bounds reading the full request, including its body. It does
	// not terminate handler computations or replace their context deadlines.
	ReadTimeout time.Duration
	// WriteTimeout bounds response socket writes, including streaming writes.
	// It does not terminate a handler that ignores write errors/cancellation.
	WriteTimeout time.Duration
	// IdleTimeout bounds waiting for another request on a keep-alive connection.
	IdleTimeout time.Duration
	// RequestTimeout bounds the request context after admission. Cooperative
	// handlers and database calls observe its cancellation. Ownership lasts
	// until actual handler exit; raw handlers own their response behavior.
	RequestTimeout time.Duration
	// ShutdownTimeout allows admitted handlers to finish before cancellation and
	// connection closure. Dependency ownership still lasts until actual exit.
	ShutdownTimeout time.Duration
	// MaxHeaderBytes configures the native net/http request-header limit.
	MaxHeaderBytes int
	// MaxBodyBytes is a positive global ceiling for known and streamed bodies.
	// Increasing it does not preallocate a buffer; raw handlers own their reads.
	MaxBodyBytes int64
	// MaxConcurrentRequests bounds handlers that have actually entered the
	// server. Zero uses DefaultServerConfig's limit. Exhaustion returns 503;
	// cancelled handlers retain their slot until they really return.
	MaxConcurrentRequests int
	// MaxConnections bounds accepted TCP connections, including connections
	// still reading headers and hijacked connections until their owner closes
	// them. Zero uses the default. Extra peers wait in the OS listen backlog.
	MaxConnections int
	// TrustTraceContext accepts valid W3C trace headers as correlation metadata.
	// It grants no identity or sampling authority. False starts a fresh trace
	// for every request; invalid or duplicate parents also start a fresh trace.
	TrustTraceContext bool
	// MaintenanceReadPaths permits GET/HEAD on at most 16 exact static paths
	// while paused or draining. Use URLs derived from protected diagnostics
	// routes. This bypasses only maintenance admission: normal route auth,
	// policies, request limits and actual server shutdown still apply.
	MaintenanceReadPaths []string `config:",json"`
	// AccessLog emits one metadata-only completion event to the server logger.
	AccessLog bool
}

// DefaultServerConfig returns a fresh configuration for a loopback HTTP server.
// TLS termination, forwarded headers and public origins are separate transport
// concerns; no incoming proxy headers are trusted by this configuration.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Address: "127.0.0.1:8080", ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second,
		IdleTimeout: time.Minute, RequestTimeout: 25 * time.Second, ShutdownTimeout: 10 * time.Second,
		MaxHeaderBytes: 32 << 10, MaxBodyBytes: 2 << 20, MaxConcurrentRequests: 1024, MaxConnections: 4096,
	}
}

// Validate checks bounds without opening a listener or resolving a hostname.
// Port zero requests an operating-system-assigned port, available through Ready.
func (c ServerConfig) Validate() error {
	_, port, err := net.SplitHostPort(c.Address)
	if err != nil || strings.TrimSpace(c.Address) != c.Address || port == "" {
		return fault.New(fault.Invalid, "HTTP address requires host:port")
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return fault.New(fault.Invalid, "HTTP port must be numeric")
		}
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fault.New(fault.Invalid, "HTTP port is out of range")
	}
	if c.ReadHeaderTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.RequestTimeout <= 0 || c.ShutdownTimeout <= 0 || c.MaxHeaderBytes <= 0 {
		return fault.New(fault.Invalid, "HTTP timeouts and header limit must be positive")
	}
	if c.MaxBodyBytes <= 0 {
		return fault.New(fault.Invalid, "HTTP body limit must be positive")
	}
	if c.MaxConcurrentRequests < 0 || c.MaxConcurrentRequests > 1<<20 {
		return fault.New(fault.Invalid, "HTTP concurrent request limit is out of range")
	}
	if c.MaxConnections < 0 || c.MaxConnections > 1<<20 {
		return fault.New(fault.Invalid, "HTTP connection limit is out of range")
	}
	return c.validateMaintenancePaths()
}
