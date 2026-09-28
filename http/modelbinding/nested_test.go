package modelbinding_test

import (
	"context"
	"errors"
	"fmt"
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
