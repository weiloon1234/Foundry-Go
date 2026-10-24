package extensionslot

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type parent struct {
	id     int
	loaded bool
}

func binding(fetch Fetch[parent, bool]) Binding[parent, bool] {
	return Binding[parent, bool]{Name: "Slot", Table: "parents", Get: func(p parent) bool { return p.loaded }, Set: func(p parent, v bool) parent { p.loaded = v; return p }, Loaded: func(v bool) bool { return v }, Count: func(bool) int { return 1 }, Fetch: fetch}
}
func TestSharedLoadingBudgetsAndAtomicAttachment(t *testing.T) {
	for _, tc := range []struct {
		name                string
		remaining, attached int
		missing, invalid    bool
		batches             []int
	}{
		{"all", 10, 10, false, false, []int{2, 2, 1}},
		{"missing", 10, 10, true, false, []int{2, 2}},
		{"row limit", 3, 10, false, true, []int{2, 2}},
		{"attachment limit", 10, 1, false, true, []int{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parents := []parent{{id: 1}, {id: 2, loaded: true}, {id: 3}, {id: 4}, {id: 5}}
			before := append([]parent(nil), parents...)
			var batches []int
			slot := New(binding(func(_ context.Context, _ database.Executor, p []parent) ([]bool, error) {
				batches = append(batches, len(p))
				out := make([]bool, len(p))
				for i := range out {
					out[i] = true
				}
				return out, nil
			}))
			rows, err := slot.Load(t.Context(), nil, parents, querycore.RelationLimits{BatchSize: 2, MaxRows: 10, MaxDepth: 8}, &tc.remaining, &tc.attached, tc.missing)
			if errors.Is(err, fault.Invalid) != tc.invalid || !reflect.DeepEqual(batches, tc.batches) {
				t.Fatalf("outcome %v, batches %v", err, batches)
			}
			if tc.invalid {
				if rows != nil || !reflect.DeepEqual(parents, before) {
					t.Fatal("failed batch attached values")
				}
				return
			}
			for _, row := range rows {
				if !row.loaded {
					t.Fatal("value was not attached")
				}
			}
		})
	}
}
func TestMalformedAndCancelledFetchesDoNotAttach(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		parents := []parent{{id: 1}, {id: 2}}
		slot := New(binding(func(context.Context, database.Executor, []parent) ([]bool, error) {
			if cancelled {
				cancel()
				return []bool{true, true}, nil
			}
			return []bool{true}, nil
		}))
		remaining, attached := 10, 10
		_, err := slot.Load(ctx, nil, parents, querycore.DefaultRelationLimits(), &remaining, &attached, false)
		cancel()
		var want error = fault.Invalid
		if cancelled {
			want = context.Canceled
		}
		if !errors.Is(err, want) || parents[0].loaded {
			t.Fatalf("fetch failure changed: %v", err)
		}
	}
}

func TestFetchBufferReusePreservesEarlierValues(t *testing.T) {
	buffer := make([]bool, 1)
	slot := New(binding(func(_ context.Context, _ database.Executor, p []parent) ([]bool, error) {
		buffer[0] = p[0].id == 2
		return buffer, nil
	}))
	remaining, attached := 10, 10
	got, err := slot.Load(t.Context(), nil, []parent{{id: 1}, {id: 2}}, querycore.RelationLimits{BatchSize: 1, MaxRows: 10, MaxDepth: 8}, &remaining, &attached, false)
	if err != nil || got[0].loaded || !got[1].loaded {
		t.Fatalf("fetch buffer alias changed earlier values: %v, %v", got, err)
	}
}
