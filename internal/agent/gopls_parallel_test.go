package agent

import "testing"

// The probes use independent server sessions and read-only consumer source.
// Cap concurrent sessions so editor acceptance stays bounded on smaller hosts;
// Go test's -parallel option can impose a lower limit without skipping probes.
const realGoplsConcurrency = 4

var realGoplsSlots = make(chan struct{}, realGoplsConcurrency)

func parallelRealGopls(t *testing.T) {
	t.Helper()
	t.Parallel()
	select {
	case realGoplsSlots <- struct{}{}:
		t.Cleanup(func() { <-realGoplsSlots })
	case <-t.Context().Done():
		t.Fatal("editor probe canceled while waiting for a server slot")
	}
}
