// Package jobs supplies an explicitly local production queue authority for
// tests. Dispatch still captures typed payloads, validates registration and
// applies deduplication; jobs execute only when a real worker is run.
package jobs

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type Harness struct {
	Backend    *memory.Backend
	Registry   *jobs.Registry
	Dispatcher *jobs.Dispatcher
	Namespace  keyspace.Namespace
}

// New owns one bounded memory authority and an independent namespace. Its
// injected clock controls availability and lease expiry, not context deadlines.
func New(t testing.TB, source clock.Clock, declarations ...jobs.Declaration) *Harness {
	t.Helper()
	config := memory.DefaultConfig()
	config.Clock = source
	backend, err := memory.New(config)
	if err != nil {
		t.Fatalf("create test job backend: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close test job backend: %v", err)
		}
	})
	registry, err := jobs.NewRegistry(declarations...)
	if err != nil {
		t.Fatalf("create test job registry: %v", err)
	}
	namespace := testkit.Namespace(t)
	dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(namespace))
	if err != nil {
		t.Fatalf("create test job dispatcher: %v", err)
	}
	return &Harness{Backend: backend, Registry: registry, Dispatcher: dispatcher, Namespace: namespace}
}

// AssertState retains the concrete job definition and execution ID. It never
// decodes or prints a private payload in assertion output.
func AssertState[P any](t testing.TB, definition jobs.Definition[P], dispatcher *jobs.Dispatcher, id jobs.ID[P], queue jobs.Queue, want jobs.State) {
	t.Helper()
	record, err := definition.Inspect(t.Context(), dispatcher, id, queue)
	if err != nil {
		t.Errorf("inspect test job state: %v", err)
		return
	}
	stored, present := record.Get()
	if !present {
		t.Error("expected job execution is absent")
		return
	}
	if stored.State != want {
		t.Errorf("job state: got %s, want %s", stored.State, want)
	}
}
