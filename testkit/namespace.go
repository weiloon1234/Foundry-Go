package testkit

import (
	"crypto/rand"
	"testing"

	"github.com/weiloon1234/Foundry-Go/keyspace"
)

// Namespace returns an independent test identity for explicitly shared cache,
// queue and coordination backends. It does not delete keys or flush a backend.
// Keep and reuse the returned value within one application assembly.
func Namespace(t testing.TB) keyspace.Namespace {
	t.Helper()
	return keyspace.Namespace{Application: "foundry-test", Environment: rand.Text()}
}
