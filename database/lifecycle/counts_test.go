package lifecycle_test

import (
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
)

func TestRegistrationCountsDoNotExposeOrConstructFactories(t *testing.T) {
	type model struct{}
	type other struct{}
	type hooks struct{}
	factory := func() hooks { t.Error("inspection invoked a factory"); return hooks{} }
	write, err := lifecycle.NewObserver[model, hooks]("write").Declare(factory)
	if err != nil {
		t.Fatal(err)
	}
	read, err := lifecycle.NewRetrievalObserver[model, hooks]("read").Declare(factory)
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := lifecycle.NewDeletionObserver[model, hooks]("delete").Declare(factory)
	if err != nil {
		t.Fatal(err)
	}
	set, err := lifecycle.NewObservers(write, read, deletion)
	if err != nil {
		t.Fatal(err)
	}
	want := lifecycle.RegistrationCounts{Write: 1, Retrieval: 1, DeletionOnly: 1}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			got := lifecycle.CountObservers[model](set)
			if got != want {
				t.Error("wrong exact-model registration counts", got)
			}
			got.Write = 99
			if lifecycle.CountObservers[model](set) != want {
				t.Error("returned count mutated registration")
			}
		})
	}
	wg.Wait()
	if got := lifecycle.CountObservers[model](set.ForDeletion()); got != (lifecycle.RegistrationCounts{Write: 2, Retrieval: 1}) {
		t.Fatal("wrong deletion view", got)
	}
	if lifecycle.CountObservers[other](set) != (lifecycle.RegistrationCounts{}) || lifecycle.CountObservers[model](lifecycle.Observers{}) != (lifecycle.RegistrationCounts{}) {
		t.Fatal("counts leaked across models or sets")
	}
}
