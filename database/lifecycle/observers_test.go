package lifecycle_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type observerModel struct{}
type otherObserverModel struct{}
type observerHooks struct {
	Name     string
	Sequence int64
}
type otherObserverHooks observerHooks

func TestObserverSetIsOrderedTypedAndImmutable(t *testing.T) {
	var calls atomic.Int64
	var declarations []lifecycle.Declaration
	for _, name := range []string{"first", "second"} {
		item, err := lifecycle.NewObserver[observerModel, observerHooks](name).Declare(func() observerHooks {
			return observerHooks{Name: name, Sequence: calls.Add(1)}
		})
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, item)
	}
	set, err := lifecycle.NewObservers(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	declarations[0] = lifecycle.Declaration{}
	factories, err := lifecycle.ObserverFactories[observerModel, observerHooks](set)
	if err != nil || len(factories) != 2 || calls.Load() != 0 {
		t.Fatalf("metadata invoked factories or lost declarations: %v", err)
	}
	if !lifecycle.HasObservers[observerModel](set) || lifecycle.HasObservers[otherObserverModel](set) {
		t.Fatal("model ownership lost")
	}
	if got := []string{factories[0]().Name, factories[1]().Name}; !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatal(got)
	}
	factories[0] = nil
	factories, err = lifecycle.ObserverFactories[observerModel, observerHooks](set)
	if err != nil || factories[0] == nil {
		t.Fatal("returned slice mutated shared set")
	}
	if first, second := factories[0](), factories[0](); first.Sequence == second.Sequence {
		t.Fatal("operation-local factory cached as a hook singleton")
	}
	if _, err := lifecycle.ObserverFactories[observerModel, otherObserverHooks](set); !errors.Is(err, fault.Invalid) {
		t.Fatal("incompatible adapter accepted", err)
	}
	empty, err := lifecycle.ObserverFactories[otherObserverModel, observerHooks](set)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("absent model must return an empty typed slice", err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			factories, err := lifecycle.ObserverFactories[observerModel, observerHooks](set)
			if err != nil || factories[0]().Name != "first" {
				t.Error("concurrent read failed", err)
			}
		})
	}
	wg.Wait()
}

func TestObserverSetRejectsInvalidDuplicateAndConflictingDeclarations(t *testing.T) {
	for _, name := range []string{"", " space", "line\nbreak", "UPPER", strings.Repeat("a", 129)} {
		key := lifecycle.NewObserver[observerModel, observerHooks](name)
		if _, err := key.Declare(func() observerHooks { return observerHooks{} }); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid name accepted", err)
		}
	}
	key := lifecycle.NewObserver[observerModel, observerHooks]("app.user.audit")
	if _, err := key.Declare(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil factory accepted", err)
	}
	one, _ := key.Declare(func() observerHooks { return observerHooks{} })
	conflict, _ := lifecycle.NewObserver[observerModel, otherObserverHooks]("different-hooks").Declare(func() otherObserverHooks { return otherObserverHooks{} })
	other, _ := lifecycle.NewObserver[otherObserverModel, observerHooks](key.Name()).Declare(func() observerHooks { return observerHooks{} })
	for _, test := range []struct {
		declarations []lifecycle.Declaration
		want         error
	}{
		{[]lifecycle.Declaration{{}}, fault.Invalid},
		{[]lifecycle.Declaration{one, one}, fault.Duplicate},
		{[]lifecycle.Declaration{one, other}, fault.Duplicate},
		{[]lifecycle.Declaration{one, conflict}, fault.Invalid},
	} {
		set, err := lifecycle.NewObservers(test.declarations...)
		if !errors.Is(err, test.want) || lifecycle.HasObservers[observerModel](set) {
			t.Fatal("invalid set published partially", err)
		}
	}
	var empty lifecycle.Observers
	factories, err := lifecycle.ObserverFactories[observerModel, observerHooks](empty)
	if err != nil || factories == nil || len(factories) != 0 {
		t.Fatal("zero set is not empty", err)
	}
}

// A deletion observer takes part only in deletions: the ordinary set neither
// reports nor dispatches it, and the deletion view joins it to the model's
// write observers in declaration order. Both kinds share the hook type.
func TestDeletionObserversJoinOnlyDeletions(t *testing.T) {
	declare := func(observer lifecycle.Observer[observerModel, observerHooks]) lifecycle.Declaration {
		t.Helper()
		item, err := observer.Declare(func() observerHooks { return observerHooks{Name: observer.Name()} })
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	deletion := declare(lifecycle.NewDeletionObserver[observerModel, observerHooks]("cleanup"))
	only, err := lifecycle.NewObservers(deletion)
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle.HasObservers[observerModel](only) || !lifecycle.HasDeletionObservers[observerModel](only) {
		t.Fatal("a deletion observer must count only for deletions")
	}
	if factories, err := lifecycle.ObserverFactories[observerModel, observerHooks](only); err != nil || len(factories) != 0 {
		t.Fatal("other writes dispatched a deletion observer", err)
	}
	set, err := lifecycle.NewObservers(declare(lifecycle.NewObserver[observerModel, observerHooks]("audit")), deletion)
	if err != nil {
		t.Fatal(err)
	}
	factories, err := lifecycle.ObserverFactories[observerModel, observerHooks](set.ForDeletion())
	if err != nil || len(factories) != 2 || factories[0]().Name != "audit" || factories[1]().Name != "cleanup" {
		t.Fatal("the deletion view lost an observer or its order", err)
	}
	if writes, err := lifecycle.ObserverFactories[observerModel, observerHooks](set); err != nil || len(writes) != 1 {
		t.Fatal("ordinary writes must dispatch only write observers", err)
	}
	conflicting, err := lifecycle.NewDeletionObserver[observerModel, otherObserverHooks]("other").Declare(func() otherObserverHooks { return otherObserverHooks{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.NewObservers(declare(lifecycle.NewObserver[observerModel, observerHooks]("audit")), conflicting); !errors.Is(err, fault.Invalid) {
		t.Fatal("write and deletion observers with different hook types were accepted", err)
	}
	var empty lifecycle.Observers
	if lifecycle.HasDeletionObservers[observerModel](empty.ForDeletion()) {
		t.Fatal("the zero set observes nothing")
	}
}
