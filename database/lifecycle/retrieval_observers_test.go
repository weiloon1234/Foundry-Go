package lifecycle_test

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type retrievalHooks struct {
	Name     string
	Sequence int64
}

func TestRetrievalFactoriesAreSeparateTypedOrderedAndImmutable(t *testing.T) {
	var reads, writes atomic.Int64
	write, err := lifecycle.NewObserver[observerModel, observerHooks]("writes").Declare(func() observerHooks {
		writes.Add(1)
		return observerHooks{Name: "write"}
	})
	if err != nil {
		t.Fatal(err)
	}
	declarations := []lifecycle.Declaration{write}
	for _, name := range []string{"first-read", "second-read"} {
		item, err := lifecycle.NewRetrievalObserver[observerModel, retrievalHooks](name).Declare(func() retrievalHooks {
			return retrievalHooks{Name: name, Sequence: reads.Add(1)}
		})
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, item)
	}
	readOnly, err := lifecycle.NewRetrievalObserver[otherObserverModel, retrievalHooks]("read-only-model").Declare(func() retrievalHooks {
		return retrievalHooks{Name: "other", Sequence: reads.Add(1)}
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(append(declarations, readOnly)...)
	if err != nil {
		t.Fatal("write/read hook types should coexist for one model", err)
	}
	if !lifecycle.HasObservers[observerModel](set) || !lifecycle.HasRetrievalObservers[observerModel](set) ||
		lifecycle.HasObservers[otherObserverModel](set) || !lifecycle.HasRetrievalObservers[otherObserverModel](set) {
		t.Fatal("read and write registration kinds were not retained")
	}
	factories, err := lifecycle.RetrievalObserverFactories[observerModel, retrievalHooks](set)
	writeFactories, writeErr := lifecycle.ObserverFactories[observerModel, observerHooks](set)
	if err != nil || writeErr != nil || len(factories) != 2 || len(writeFactories) != 1 || reads.Load() != 0 || writes.Load() != 0 {
		t.Fatal("inspection invoked or mixed factories", err, writeErr)
	}
	if got := []string{factories[0]().Name, factories[1]().Name}; !reflect.DeepEqual(got, []string{"first-read", "second-read"}) {
		t.Fatal("retrieval declaration order changed", got)
	}
	factories[0] = nil
	factories, err = lifecycle.RetrievalObserverFactories[observerModel, retrievalHooks](set)
	if err != nil || factories[0] == nil || factories[0]().Sequence == factories[0]().Sequence {
		t.Fatal("retrieval slice or operation-local factory was cached", err)
	}
	if _, err := lifecycle.RetrievalObserverFactories[observerModel, observerHooks](set); !errors.Is(err, fault.Invalid) {
		t.Fatal("read lookup accepted the write hook type", err)
	}
	if _, err := lifecycle.ObserverFactories[observerModel, retrievalHooks](set); !errors.Is(err, fault.Invalid) {
		t.Fatal("write lookup accepted the read hook type", err)
	}
	empty, err := lifecycle.ObserverFactories[otherObserverModel, observerHooks](set)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("retrieval-only model entered write dispatch", err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			factories, err := lifecycle.RetrievalObserverFactories[observerModel, retrievalHooks](set)
			if err != nil || len(factories) != 2 || factories[0]().Name != "first-read" {
				t.Error("concurrent retrieval lookup changed", err)
			}
		})
	}
	wg.Wait()
	if writes.Load() != 0 {
		t.Fatal("retrieval instantiated write factories")
	}
	writeFactories[0]()
	if writes.Load() != 1 {
		t.Fatal("normal write factory lost its registration")
	}
}

func TestRetrievalRegistrationsRetainPoolWideNamesAndKindLocalTypes(t *testing.T) {
	readKey := lifecycle.NewRetrievalObserver[observerModel, retrievalHooks]("shared")
	read, err := readKey.Declare(func() retrievalHooks { return retrievalHooks{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readKey.Declare(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil retrieval factory accepted", err)
	}
	if _, err := lifecycle.NewRetrievalObserver[observerModel, retrievalHooks]("").Declare(func() retrievalHooks { return retrievalHooks{} }); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid retrieval name accepted", err)
	}
	write, _ := lifecycle.NewObserver[observerModel, observerHooks](readKey.Name()).Declare(func() observerHooks { return observerHooks{} })
	conflict, _ := lifecycle.NewRetrievalObserver[observerModel, otherObserverHooks]("different-read-type").Declare(func() otherObserverHooks { return otherObserverHooks{} })
	for _, test := range []struct {
		declaration lifecycle.Declaration
		want        error
	}{
		{write, fault.Duplicate},
		{read, fault.Duplicate},
		{conflict, fault.Invalid},
	} {
		set, err := lifecycle.NewObservers(read, test.declaration)
		if !errors.Is(err, test.want) || lifecycle.HasObservers[observerModel](set) || lifecycle.HasRetrievalObservers[observerModel](set) {
			t.Fatal("invalid observer set was published partially", err)
		}
	}
	var zero lifecycle.Observers
	factories, err := lifecycle.RetrievalObserverFactories[observerModel, retrievalHooks](zero)
	if err != nil || factories == nil || len(factories) != 0 || lifecycle.HasRetrievalObservers[observerModel](zero) {
		t.Fatal("zero retrieval registry was not empty", err)
	}
}
