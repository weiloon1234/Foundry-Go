package modelbinding_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/value"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedResolversKeepAllModelsAndShortCircuit(t *testing.T) {
	var parents, children, grandchildren atomic.Int32
	parent := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
		parents.Add(1)
		if p.Member == 0 {
			return value.Optional[member]{}, nil
		}
		return value.Set(member{ID: p.Member}), nil
	})
	child := modelbinding.Then(parent, func(_ context.Context, p path, m member) (value.Optional[string], error) {
		children.Add(1)
		if p.Member != m.ID {
			t.Error("parent/path identity diverged")
		}
		if p.Member < 0 {
			return value.Optional[string]{}, nil
		}
		return value.Set(fmt.Sprint(m.ID)), nil
	})
	nested := modelbinding.Then(child, func(_ context.Context, p path, previous modelbinding.Models[member, string]) (value.Optional[int64], error) {
		grandchildren.Add(1)
		if previous.Parent.ID != p.Member || previous.Child != fmt.Sprint(p.Member) {
			t.Error("lost earlier models")
		}
		return value.Set(int64(p.Member)), nil
	})
	for _, id := range []memberKey{0, -1} {
		got, err := nested.Resolve(t.Context(), path{id})
		if !errors.Is(err, foundryhttp.NotFound) || got.Parent.Parent.ID != 0 || got.Child != 0 {
			t.Fatal("partial bundle escaped", err)
		}
	}
	if parents.Load() != 2 || children.Load() != 1 || grandchildren.Load() != 0 {
		t.Fatal("missing parent did not short circuit")
	}
	var group sync.WaitGroup
	for i := memberKey(1); i <= 24; i++ {
		group.Go(func() {
			got, err := nested.Resolve(t.Context(), path{i})
			if err != nil || got.Parent.Parent.ID != i || got.Child != int64(i) {
				t.Error("shared or missing result", err)
			}
		})
	}
	group.Wait()
	if parents.Load() != 26 || children.Load() != 25 || grandchildren.Load() != 24 {
		t.Fatal("resolver repeated or cached across requests")
	}
}
func TestNestedResolverFailureAndCancellationRemainOwned(t *testing.T) {
	parent := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
		return value.Set(member{ID: p.Member}), nil
	})
	for _, mode := range []string{"error", "panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			nested := modelbinding.Then(parent, func(context.Context, path, member) (value.Optional[string], error) {
				switch mode {
				case "error":
					return value.Set("partial"), errors.New("failed child")
				case "panic":
					panic("failed child")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					close(entered)
					<-release
				}
				return value.Set("child"), nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			go func() {
				defer close(done)
				got, err := nested.Resolve(ctx, path{42})
				if err == nil || got.Parent.ID != 0 || got.Child != "" {
					t.Error("failed child published partial result")
				}
			}()
			if mode == "cancel" {
				<-entered
				cancel()
				select {
				case <-done:
					t.Fatal("callback was abandoned")
				case <-time.After(20 * time.Millisecond):
				}
				close(release)
			}
			<-done
		})
	}
	if modelbinding.Then(parent, (func(context.Context, path, member) (value.Optional[string], error))(nil)).Validate() == nil {
		t.Fatal("nil child accepted")
	}
}
func TestBoundResourceAuthorizationBeforeHandler(t *testing.T) {
	for _, mode := range []string{"allow", "deny", "panic", "goexit", "error"} {
		t.Run(mode, func(t *testing.T) {
			resolved, authorized, handled := 0, 0, 0
			resolver := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
				resolved++
				return value.Set(member{ID: p.Member}), nil
			})
			bound := modelbinding.Bind(endpoint(), resolver)
			if bound.WithAuthorization(nil).Validate() == nil {
				t.Fatal("nil policy accepted")
			}
			bound = bound.WithAuthorization(func(_ context.Context, in input) error {
				authorized++
				if in.Model.ID != 42 {
					t.Error("unbound policy input")
				}
				switch mode {
				case "deny":
					return foundryhttp.Forbidden
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "error":
					return errors.New("private")
				}
				return nil
			})
			r := router(t, bound.Handle(func(context.Context, input) (foundryhttp.NoContent, error) {
				handled++
				return foundryhttp.NoContent{}, nil
			}))
			res := serve(r, "/members/42")
			want := 500
			if mode == "allow" {
				want = 204
			}
			if mode == "deny" {
				want = 403
			}
			if res.Code != want || resolved != 1 || authorized != 1 || (handled == 1) != (mode == "allow") {
				t.Fatal("resource policy stage lost", res.Code, resolved, authorized, handled)
			}
		})
	}
}

type countedQuery struct{ validations *atomic.Int32 }

func (q countedQuery) Validate() error { q.validations.Add(1); return nil }
func (q countedQuery) Find(_ context.Context, _ database.Executor, key memberKey) (value.Optional[member], error) {
	return value.Set(member{ID: key}), nil
}

func TestNestedBindingValidatesOnceAtRegistration(t *testing.T) {
	var validations atomic.Int32
	resolver := modelbinding.ByKey(&neverExecutor{}, countedQuery{&validations}, func(p path) memberKey { return p.Member })
	nested := modelbinding.Then(resolver, func(_ context.Context, p path, m member) (value.Optional[string], error) {
		return value.Set(fmt.Sprint(m.ID)), nil
	})
	deeper := modelbinding.Then(nested, func(_ context.Context, p path, previous modelbinding.Models[member, string]) (value.Optional[int64], error) {
		return value.Set(int64(previous.Parent.ID)), nil
	})
	type deepInput = modelbinding.Input[path, foundryhttp.NoQuery, foundryhttp.NoBody, modelbinding.Models[modelbinding.Models[member, string], int64]]
	r := router(t, modelbinding.Bind(endpoint(), deeper).Handle(func(context.Context, deepInput) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	registered := validations.Load()
	if registered == 0 {
		t.Fatal("registration did not validate the key query")
	}
	for range 5 {
		if w := serve(r, "/members/7"); w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if validations.Load() != registered {
		t.Fatalf("requests revalidated nested declarations: %d -> %d", registered, validations.Load())
	}
	// Direct Resolve validates each ancestor once per call, not once per level.
	if _, err := deeper.Resolve(t.Context(), path{7}); err != nil || validations.Load() != registered+1 {
		t.Fatalf("direct resolve validations=%d err=%v", validations.Load()-registered, err)
	}
}

func TestMissingHandlerCustomizesAbsentModels(t *testing.T) {
	gone := errors.New("member gone")
	resolver := modelbinding.Define(func(_ context.Context, p path) (value.Optional[member], error) {
		if p.Member == 404 {
			return value.Optional[member]{}, nil
		}
		return value.Set(member{ID: p.Member}), nil
	})
	custom := resolver.WithMissing(func(_ context.Context, p path) error {
		if p.Member != 404 {
			t.Error("missing handler received another path")
		}
		return foundryhttp.Conflict.WithCause(gone)
	})
	if _, err := custom.Resolve(t.Context(), path{404}); !errors.Is(err, foundryhttp.Conflict) || !errors.Is(err, gone) {
		t.Fatal("custom missing error lost", err)
	}
	if got, err := custom.Resolve(t.Context(), path{5}); err != nil || got.ID != 5 {
		t.Fatal("present model changed", err)
	}
	fallback := resolver.WithMissing(func(context.Context, path) error { return nil })
	if _, err := fallback.Resolve(t.Context(), path{404}); !errors.Is(err, foundryhttp.NotFound) {
		t.Fatal("nil missing result did not remain 404", err)
	}
	panicking := resolver.WithMissing(func(context.Context, path) error { panic("private") })
	if _, err := panicking.Resolve(t.Context(), path{404}); !errors.Is(err, foundryhttp.InternalError) {
		t.Fatal("missing handler panic escaped", err)
	}
	if resolver.WithMissing(nil).Validate() == nil {
		t.Fatal("nil missing handler accepted")
	}
}
