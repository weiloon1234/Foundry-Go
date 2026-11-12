// Package redis provides opt-in Redis acceptance helpers. Passing tests remove
// only their own registered key prefixes after application shutdown. Failed or
// panicking tests retain them, as does FOUNDRY_TEST_KEEP_REDIS_KEYS=1. No helper
// flushes a shared store. Callers must use unique test-owned namespaces.
package redis

import (
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
)

const (
	AddressVariable  = "FOUNDRY_TEST_REDIS_ADDR"
	RequiredVariable = "FOUNDRY_TEST_REDIS_REQUIRED"
	UserVariable     = "FOUNDRY_TEST_REDIS_USER"
	PasswordVariable = "FOUNDRY_TEST_REDIS_PASSWORD"
)

// Config reads the explicitly opted-in plaintext endpoint (host:port) over the
// default client limits, plus the optional UserVariable and PasswordVariable
// credentials. Each is read independently: a password alone authenticates
// Redis's default user. The password is kept in a secret-bearing field and is
// never logged. Ordinary test runs skip without the address; the required
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
	c.User, c.Password = os.Getenv(UserVariable), secret.New(os.Getenv(PasswordVariable))
	return c
}
