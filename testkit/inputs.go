package testkit

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/testinputs"
)

// TrackExternalInputs makes Go's test cache depend on make's current source/tool
// fingerprint. Use it in tests invoking a compiler, generator, gopls or Node over
// external-module inputs. Make computes the fingerprint once per invocation.
// For a direct go test invocation without make, use -count=1 for these gates:
// Go does not track child file reads outside the parent test's module root.
func TrackExternalInputs(t testing.TB) {
	t.Helper()
	fingerprint := os.Getenv(testinputs.Variable)
	if fingerprint == "" {
		return
	}
	bytes, err := hex.DecodeString(fingerprint)
	if err != nil || len(bytes) != 32 {
		t.Fatal("invalid external test-input fingerprint; use make or go test -count=1")
	}
}
