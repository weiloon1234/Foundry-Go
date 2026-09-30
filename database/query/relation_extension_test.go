package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// slotValue is a loaded external value, held per record by the test binding.
type slotValue struct {
	text   string
	loaded bool
}

type refusingExecutor struct{}

func (refusingExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	return database.Result{}, errors.New("extension slots must not use the parent executor here")
}
func (refusingExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	return nil, errors.New("extension slots must not use the parent executor here")
}

func slotBinding(fetch SlotFetch[identityRecord, slotValue], slots map[int64]slotValue) ExtensionBinding[identityRecord, slotValue] {
	return ExtensionBinding[identityRecord, slotValue]{
		Name: "Slot", Table: "identity_records",
		Get: func(m identityRecord) slotValue { return slots[m.ID] },
		Set: func(m identityRecord, v slotValue) identityRecord {
			slots[m.ID] = v
			return m
		},
		Loaded: func(v slotValue) bool { return v.loaded },
		Count: func(v slotValue) int {
			if v.loaded {
				return 1
			}
			return 0
		},
		Fetch: fetch,
	}
}

func TestExtensionSlotLoadsInRelationBatchesWithinTheBudget(t *testing.T) {
	slots := map[int64]slotValue{}
	var batches []int
	binding := slotBinding(func(_ context.Context, _ database.Executor, parents []identityRecord) ([]slotValue, error) {
		batches = append(batches, len(parents))
		result := make([]slotValue, len(parents))
		for i, parent := range parents {
			result[i] = slotValue{text: strings.Repeat("x", int(parent.ID)), loaded: true}
		}
		return result, nil
	}, slots)
	parents := []identityRecord{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	limits := RelationLimits{BatchSize: 2, MaxRows: 100, MaxDepth: 8}
	if _, err := identityQuery().WithRelationLimits(limits).With(NewExtensionSlot(binding)).Load(t.Context(), refusingExecutor{}, parents); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 || batches[0] != 2 || batches[2] != 1 || slots[5].text != "xxxxx" {
		t.Fatal("parent batches or values", batches, slots)
	}
	// LoadMissing skips loaded slots and fetches nothing.
	batches = nil
	if _, err := identityQuery().With(NewExtensionSlot(binding)).LoadMissing(t.Context(), refusingExecutor{}, parents); err != nil || len(batches) != 0 {
		t.Fatal("LoadMissing fetched loaded slots", batches, err)
	}
	// The budget is charged per batch, so an over-budget load stops early.
	batches = nil
	clear(slots)
	tight := RelationLimits{BatchSize: 2, MaxRows: 3, MaxDepth: 8}
	if _, err := identityQuery().WithRelationLimits(tight).With(NewExtensionSlot(binding)).Load(t.Context(), refusingExecutor{}, parents); !errors.Is(err, fault.Invalid) || len(batches) != 2 {
		t.Fatal("over-budget load was not stopped after the batch that exceeded it", batches, err)
	}
}

func TestExtensionSlotValidationPrecedesLoading(t *testing.T) {
	slots := map[int64]slotValue{}
	fetched := false
	fetch := func(context.Context, database.Executor, []identityRecord) ([]slotValue, error) {
		fetched = true
		return []slotValue{}, nil
	}
	unbound := slotBinding(nil, slots)
	unbound.Unbound = fault.New(fault.Missing, "slot is not bound")
	wrongTable := slotBinding(fetch, slots)
	wrongTable.Table = "other_records"
	incomplete := slotBinding(fetch, slots)
	incomplete.Get = nil
	for name, test := range map[string]struct {
		relation Relation[identityRecord]
		want     error
	}{
		"unbound":        {NewExtensionSlot(unbound), fault.Missing},
		"other table":    {NewExtensionSlot(wrongTable), fault.Invalid},
		"incomplete":     {NewExtensionSlot(incomplete), fault.Invalid},
		"zero":           {ExtensionSlot[identityRecord]{}, fault.Invalid},
		"misaligned":     {NewExtensionSlot(slotBinding(fetch, slots)), fault.Invalid},
		"repeated names": {nil, fault.Invalid},
	} {
		t.Run(name, func(t *testing.T) {
			fetched = false
			q := identityQuery().With(test.relation)
			if test.relation == nil {
				q = identityQuery().With(NewExtensionSlot(slotBinding(fetch, slots)), NewExtensionSlot(slotBinding(fetch, slots)))
			}
			_, err := q.Load(t.Context(), refusingExecutor{}, []identityRecord{{ID: 1}})
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
			if name != "misaligned" && fetched {
				t.Fatal("a slot fetched before validation rejected it")
			}
		})
	}
	fetched = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := identityQuery().With(NewExtensionSlot(slotBinding(fetch, slots))).Load(ctx, refusingExecutor{}, []identityRecord{{ID: 1}}); !errors.Is(err, context.Canceled) || fetched {
		t.Fatal("a cancelled load fetched", err)
	}
}
