// Package redis provides opt-in, non-destructive Redis acceptance helpers.
// Callers scope their keys to their own namespace; no helper flushes or deletes
// shared data.
package redis

import (
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/weiloon1234/Foundry-Go/redis"
)

const (
	AddressVariable  = "FOUNDRY_TEST_REDIS_ADDR"
	RequiredVariable = "FOUNDRY_TEST_REDIS_REQUIRED"
)

// Config reads the explicitly opted-in plaintext endpoint (host:port) over the
// default client limits. Ordinary test runs skip without it; the required
// acceptance command fails if it is missing.
func Config(t testing.TB) redis.Config {
	t.Helper()
	address := os.Getenv(AddressVariable)
	if address == "" {
		if os.Getenv(RequiredVariable) == "1" {
			t.Fatal("required Redis acceptance endpoint is missing")
		}
		t.Skip("Redis acceptance is opt-in; set " + AddressVariable)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal("invalid Redis test endpoint")
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal("invalid Redis test port")
	}
	c := redis.DefaultConfig()
	c.Host, c.Port, c.TLS = host, uint16(number), redis.DisableTLS
	return c
}
