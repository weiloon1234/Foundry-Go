package querycore

import (
	"errors"
	"runtime"
	"testing"
	"testing/synctest"

	"github.com/weiloon1234/Foundry-Go/fault"
)

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
