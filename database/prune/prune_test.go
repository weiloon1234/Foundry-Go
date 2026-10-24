package prune

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRegistryBoundsBeforeBatchExecution(t *testing.T) {
	calls := 0
	r, err := New(Target{name: "app.logs", mode: Mass, run: func(context.Context, database.Transactor, int) (int64, error) {
		calls++
		return 0, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	db := new(database.DB)
	for _, options := range []Options{{}, {BatchSize: -1, MaxBatches: 1}, {BatchSize: orm.MaxPerModelWriteRows + 1, MaxBatches: 1}, {BatchSize: 1, MaxBatches: 0}} {
		if _, err := r.Run(t.Context(), db, options); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid prune bounds accepted", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid bounds ran a batch")
	}
	if _, err := r.Run(t.Context(), db, Options{BatchSize: orm.MaxPerModelWriteRows, MaxBatches: 1}); err != nil || calls != 1 {
		t.Fatal("exact bound rejected", err, calls)
	}
}

func TestRegistryReportsOnlyConfirmedBatchesAndStops(t *testing.T) {
	failure := errors.New("batch failed")
	var calls []Name
	target := func(name Name, run func() (int64, error)) Target {
		return Target{name: name, mode: Lifecycle, run: func(context.Context, database.Transactor, int) (int64, error) {
			calls = append(calls, name)
			return run()
		}}
	}
	batches := 0
	r, err := New(
		target("app.c", func() (int64, error) { t.Fatal("ran after failure"); return 0, nil }),
		target("app.b", func() (int64, error) {
			batches++
			if batches == 2 {
				return 99, failure
			}
			return 2, nil
		}),
		target("app.a", func() (int64, error) { return 2, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(t.Context(), new(database.DB), Options{BatchSize: 2, MaxBatches: 2})
	if !errors.Is(err, failure) || result.StoppedAt == nil || *result.StoppedAt != "app.b" {
		t.Fatal("lost batch failure", result, err)
	}
	if !reflect.DeepEqual(result.Counts, []Count{{Name: "app.a", Removed: 4, Remaining: true}, {Name: "app.b", Removed: 2}}) || !reflect.DeepEqual(calls, []Name{"app.a", "app.a", "app.b", "app.b"}) {
		t.Fatal("reported unconfirmed work or lost ordered bounds", result, calls)
	}
}
