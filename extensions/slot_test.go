package extensions_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLoadInPartsKeepsOrderAndPartSizes(t *testing.T) {
	parents := []int{1, 2, 3, 4, 5, 6, 7}
	var sizes []int
	values, err := extensions.LoadInParts(t.Context(), parents, 3, nil, func(_ context.Context, part []int) ([]int, error) {
		sizes = append(sizes, len(part))
		result := make([]int, len(part))
		for i, parent := range part {
			result[i] = parent * 10
		}
		return result, nil
	})
	if err != nil || !slices.Equal(values, []int{10, 20, 30, 40, 50, 60, 70}) || !slices.Equal(sizes, []int{3, 3, 1}) {
		t.Fatal("parts or order changed", values, sizes, err)
	}
}

func TestLoadInPartsHalvesOnlyOnTheLimit(t *testing.T) {
	limit := fault.New(fault.Conflict, "batch limit")
	var sizes []int
	values, err := extensions.LoadInParts(t.Context(), []int{1, 2, 3, 4, 5}, 5, limit, func(_ context.Context, part []int) ([]int, error) {
		sizes = append(sizes, len(part))
		if len(part) > 2 {
			return nil, limit
		}
		return slices.Clone(part), nil
	})
	if err != nil || !slices.Equal(values, []int{1, 2, 3, 4, 5}) || !slices.Equal(sizes, []int{5, 2, 3, 1, 2}) {
		t.Fatal("halving did not load every parent in order", values, sizes, err)
	}
	if _, err := extensions.LoadInParts(t.Context(), []int{1}, 5, limit, func(context.Context, []int) ([]int, error) { return nil, limit }); !errors.Is(err, limit) {
		t.Fatal("a single owner beyond the limit must fail", err)
	}
	other := fault.New(fault.Conflict, "stored version differs")
	calls := 0
	if _, err := extensions.LoadInParts(t.Context(), []int{1, 2, 3}, 3, limit, func(context.Context, []int) ([]int, error) {
		calls++
		return nil, other
	}); !errors.Is(err, other) || calls != 1 {
		t.Fatal("another failure must not be retried", err, calls)
	}
	if _, err := extensions.LoadInParts(t.Context(), []int{1, 2}, 2, limit, func(context.Context, []int) ([]int, error) { return []int{1}, nil }); !errors.Is(err, fault.Invalid) {
		t.Fatal("a misaligned result was accepted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := extensions.LoadInParts(ctx, []int{1}, 1, limit, func(context.Context, []int) ([]int, error) { return []int{1}, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("a cancelled load ran", err)
	}
}
