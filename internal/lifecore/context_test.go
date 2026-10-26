package lifecore

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestModelHookNestingBoundPreservesContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "origin")
	for range MaxDepth {
		var e error
		ctx, e = Context(ctx)
		if e != nil || ctx.Value(key{}) != "origin" {
			t.Fatal("nesting lost request", e)
		}
	}
	if _, e := Context(ctx); !errors.Is(e, fault.Invalid) {
		t.Fatal("unbounded callback depth", e)
	}
	if _, e := Context(t.Context()); e != nil {
		t.Fatal("depth leaked between requests", e)
	}
}
