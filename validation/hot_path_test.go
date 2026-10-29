package validation_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func validPatch() Patch {
	return Patch{Name: "member", Nickname: value.Set(value.Of("ok")), Tags: []string{"one", "two", "three"}}
}

// A tree of framework rules runs inline: valid input allocates only the
// execution state, never issue storage, JSON Pointer paths or a goroutine.
func TestValidInputAvoidsIssueAndPathAllocations(t *testing.T) {
	rules := patchRules()
	ctx, input, limits := t.Context(), validPatch(), validation.DefaultLimits()
	allocations := testing.AllocsPerRun(200, func() {
		if err := validation.Check(rules, ctx, input, limits); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 1 {
		t.Fatalf("valid check allocated %.1f times", allocations)
	}
	// Rejections still materialize escaped paths.
	issues := rejection(t, validation.Check(rules, ctx, Patch{Name: " ", Tags: []string{"ok", " "}}, limits)).Issues()
	if issues[0].Path != "/display~0~1name" || issues[len(issues)-1].Path != "/tags/1" {
		t.Fatalf("materialized paths: %+v", issues)
	}
}

// exitsCaller reports whether check ended its goroutine through Goexit instead
// of returning.
func exitsCaller(check func() error) bool {
	returned := make(chan bool, 1)
	go func() {
		completed := false
		defer func() { returned <- completed }()
		_ = check()
		completed = true
	}()
	return !<-returned
}

func TestGoexitContainmentFollowsApplicationCallbacks(t *testing.T) {
	selector := validation.DefineField("name", func(Patch) string { runtime.Goexit(); return "" })
	inline := validation.Rules(selector, validation.NonBlank[string]())
	// Selectors and value methods run inline like any Go call; panics are still
	// contained, but Goexit ends the caller.
	if !exitsCaller(func() error {
		return validation.Check(inline, context.Background(), Patch{}, validation.DefaultLimits())
	}) {
		t.Fatal("inline selector Goexit was unexpectedly contained")
	}
	panicking := validation.Rules(validation.DefineField("name", func(Patch) string { panic("private") }), validation.NonBlank[string]())
	if err := validation.Check(panicking, t.Context(), Patch{}, validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("inline panic escaped", err)
	}
	// A tree with an application callback runs in one owned goroutine, so the
	// same selector Goexit is an internal failure.
	custom := validation.Custom(validation.Spec{ID: "app.any", Message: "Any."}, func(context.Context, Patch) (bool, error) { return true, nil })
	owned := validation.All(custom, inline)
	var err error
	if exitsCaller(func() error {
		err = validation.Check(owned, context.Background(), Patch{}, validation.DefaultLimits())
		return err
	}) || !errors.Is(err, fault.Internal) {
		t.Fatal("callback tree did not contain Goexit", err)
	}
}

func BenchmarkValidCheck(b *testing.B) {
	rules := patchRules()
	ctx, input, limits := b.Context(), validPatch(), validation.DefaultLimits()
	b.ReportAllocs()
	for b.Loop() {
		if err := validation.Check(rules, ctx, input, limits); err != nil {
			b.Fatal(err)
		}
	}
}
