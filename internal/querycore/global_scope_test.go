package querycore

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLazyScopeSourceFailureIsSticky(t *testing.T) {
	for _, mode := range []string{"panic", "goexit", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var exited atomic.Bool
			lazy := &LazyScopes{Source: func() []ScopeDefinition {
				defer exited.Store(true)
				calls.Add(1)
				switch mode {
				case "panic":
					panic("private tenant value")
				case "goexit":
					runtime.Goexit()
				}
				return []ScopeDefinition{{Name: "invalid"}}
			}}
			want := fault.Panicked
			if mode == "invalid" {
				want = fault.Invalid
			}
			_, first := lazy.Get("records")
			if !errors.Is(first, want) || !lazy.Ready.Load() || !exited.Load() {
				t.Fatal("source failure was not published", first)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					_, err := lazy.Get("records")
					if err != first || !errors.Is(err, want) {
						t.Error("cached source failure changed", err)
					}
					if strings.Contains(fmt.Sprintf("%s %v %+v %#v", err.Error(), err, err, err), "private tenant value") {
						t.Error("source failure disclosed panic value")
					}
				})
			}
			wg.Wait()
			if calls.Load() != 1 {
				t.Fatal("failed source was retried", calls.Load())
			}
		})
	}
}

func TestLazyScopesWaitsForFailedSourceExit(t *testing.T) {
	for _, mode := range []string{"goexit", "panic"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				unwinding, release := make(chan struct{}), make(chan struct{})
				calls := 0
				lazy := &LazyScopes{Source: func() []ScopeDefinition {
					calls++
					defer func() { close(unwinding); <-release }()
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("private scope payload")
				}}
				results := make(chan error, 1)
				get := func() {
					var err error
					defer func() { results <- err }()
					_, err = lazy.Get("records")
				}
				go get()
				<-unwinding
				synctest.Wait()
				if lazy.Ready.Load() || len(results) != 0 {
					t.Error("scope source published before its callback exited")
				}
				close(release)
				if err := <-results; !errors.Is(err, fault.Panicked) {
					t.Error("source failure lost", err)
				}
				if scopes, err := lazy.Get("records"); !errors.Is(err, fault.Panicked) || len(scopes) != 0 || !lazy.Ready.Load() || calls != 1 {
					t.Fatal("failed source was not sticky", scopes, err, calls)
				}
			})
		})
	}
}
