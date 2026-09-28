package http_test

import (
	"errors"
	stdhttp "net/http"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestServerConfigurationRejectsUnboundedOrMalformedSettings(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*foundryhttp.ServerConfig)
	}{
		{"empty address", func(c *foundryhttp.ServerConfig) { c.Address = "" }},
		{"missing port", func(c *foundryhttp.ServerConfig) { c.Address = "localhost" }},
		{"named port", func(c *foundryhttp.ServerConfig) { c.Address = "localhost:http" }},
		{"port range", func(c *foundryhttp.ServerConfig) { c.Address = ":65536" }},
		{"negative port", func(c *foundryhttp.ServerConfig) { c.Address = ":-1" }},
		{"address whitespace", func(c *foundryhttp.ServerConfig) { c.Address = " :80" }},
		{"header timeout", func(c *foundryhttp.ServerConfig) { c.ReadHeaderTimeout = 0 }},
		{"read timeout", func(c *foundryhttp.ServerConfig) { c.ReadTimeout = 0 }},
		{"write timeout", func(c *foundryhttp.ServerConfig) { c.WriteTimeout = -1 }},
		{"request timeout", func(c *foundryhttp.ServerConfig) { c.RequestTimeout = 0 }},
		{"idle timeout", func(c *foundryhttp.ServerConfig) { c.IdleTimeout = 0 }},
		{"shutdown timeout", func(c *foundryhttp.ServerConfig) { c.ShutdownTimeout = 0 }},
		{"header bytes", func(c *foundryhttp.ServerConfig) { c.MaxHeaderBytes = 0 }},
		{"body bytes", func(c *foundryhttp.ServerConfig) { c.MaxBodyBytes = 0 }},
		{"negative concurrency", func(c *foundryhttp.ServerConfig) { c.MaxConcurrentRequests = -1 }},
		{"concurrency ceiling", func(c *foundryhttp.ServerConfig) { c.MaxConcurrentRequests = 1<<20 + 1 }},
		{"negative connections", func(c *foundryhttp.ServerConfig) { c.MaxConnections = -1 }},
		{"connection ceiling", func(c *foundryhttp.ServerConfig) { c.MaxConnections = 1<<20 + 1 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			c := config()
			change.apply(&c)
			if err := c.Validate(); !errors.Is(err, fault.Invalid) {
				t.Fatalf("invalid setting accepted: %v", err)
			}
		})
	}
	for _, address := range []string{"127.0.0.1:0", "localhost:8080", "[::1]:8080", ":8080"} {
		c := config()
		c.Address = address
		if err := c.Validate(); err != nil {
			t.Errorf("valid bind address %q: %v", address, err)
		}
	}
	var nilHandler stdhttp.HandlerFunc
	for _, handler := range []stdhttp.Handler{nil, nilHandler} {
		if _, err := foundryhttp.Prepare(handler, config(), quietLogger()); !errors.Is(err, fault.Invalid) {
			t.Fatalf("nil handler accepted: %v", err)
		}
	}
	if _, err := foundryhttp.Prepare(stdhttp.NotFoundHandler(), config(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil logger accepted: %v", err)
	}
}
