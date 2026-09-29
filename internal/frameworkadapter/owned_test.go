package frameworkadapter_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
)

// embedded inherits FoundryAdapter by promotion but overrides adapter I/O.
type embedded struct{ *memory.Backend }

func (embedded) Get(context.Context, cache.EntryKey) ([]byte, bool, error) {
	runtime.Goexit()
	return nil, false, nil
}

func TestPromotedMarkerIsNotFrameworkOwned(t *testing.T) {
	backend, err := memory.New(memory.DefaultConfig(), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	if !frameworkadapter.Is(backend) {
		t.Fatal("framework adapter was not recognized")
	}
	wrapped := embedded{backend}
	for _, adapter := range []any{wrapped, &wrapped, nil, struct{}{}} {
		if frameworkadapter.Is(adapter) {
			t.Fatal("adapter outside the framework was trusted", adapter)
		}
	}
	// The embedding type's own method stays isolated: its Goexit is contained.
	err = frameworkadapter.Call(wrapped, "adapter", func() error {
		_, _, err := wrapped.Get(t.Context(), cache.EntryKey{})
		return err
	})
	if err == nil {
		t.Fatal("contained Goexit reported success")
	}
}
