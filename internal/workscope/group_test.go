package workscope

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestIsolationCancellationAndSelfClose(t *testing.T) {
	g, err := New(1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close(context.Background())
	for _, mode := range []string{"panic", "goexit", "close"} {
		err := g.Run(t.Context(), "test", func(ctx context.Context) error {
			switch mode {
			case "panic":
				panic("private")
			case "goexit":
				runtime.Goexit()
			case "close":
				return g.Close(ctx)
			}
			return nil
		})
		if err == nil {
			t.Fatal("invalid callback accepted")
		}
		if mode == "close" && !errors.Is(err, fault.Cycle) {
			t.Fatal("self close deadlock guard", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	if err := g.Run(ctx, "canceled", func(context.Context) error { ran = true; return nil }); !errors.Is(err, context.Canceled) || ran {
		t.Fatal("canceled callback invoked", err)
	}
	if err := g.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := g.Run(t.Context(), "closed", func(context.Context) error { return nil }); err == nil {
		t.Fatal("closed service admitted work")
	}
}
