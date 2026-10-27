package querycore

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RelationLimits bounds key batches, fetched/attached rows and nesting.
type RelationLimits struct{ BatchSize, MaxRows, MaxDepth int }

func DefaultRelationLimits() RelationLimits { return RelationLimits{500, 10000, 8} }
func (l RelationLimits) Validate() error {
	if l.BatchSize < 1 || l.BatchSize > 1000 || l.MaxRows < 1 || l.MaxRows > 1000000 || l.MaxDepth < 1 || l.MaxDepth > 32 {
		return safeerror.NewFault(fault.Invalid, "invalid relation resource limits")
	}
	return nil
}

// ConsumeRelationBudget charges fetched rows or attached values independently.
func ConsumeRelationBudget(remaining *int, count int, attached bool) error {
	if count > *remaining {
		if attached {
			return safeerror.NewFault(fault.Invalid, "attached related models exceed the shared loading budget")
		}
		return safeerror.NewFault(fault.Invalid, "related rows exceed the shared loading budget")
	}
	*remaining -= count
	return nil
}

// RelationKeys preserves parent positions and first-seen unique key order.
// Typed adapters retain decoded values at the returned unique indices.
func RelationKeys[K any](ctx context.Context, count, batchSize int, read func(int) (K, sqlvalue.Value, bool, error)) ([]value.Optional[sqlvalue.Value], []K, map[sqlvalue.Value]bool, error) {
	positions := make([]value.Optional[sqlvalue.Value], count)
	keys := make([]K, 0, count)
	seen := make(map[sqlvalue.Value]bool)
	for i := 0; i < count; i++ {
		if i%batchSize == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		typed, key, present, err := read(i)
		if err != nil {
			return nil, nil, nil, err
		}
		if present {
			positions[i] = value.Set(key)
			if !seen[key] {
				seen[key] = true
				keys = append(keys, typed)
			}
		}
	}
	return positions, keys, seen, nil
}

// FetchRelationBatches enforces the shared row budget with a one-row sentinel.
func FetchRelationBatches(count, batchSize int, remaining *int, fetch func(start, end, limit int) (int, error)) error {
	for start := 0; start < count; start += batchSize {
		n, err := fetch(start, min(start+batchSize, count), *remaining+1)
		if err != nil {
			return err
		}
		if err := ConsumeRelationBudget(remaining, n, false); err != nil {
			return err
		}
	}
	return nil
}

// GroupRelationRows checks every returned key before associating rows to parents.
func GroupRelationRows[N any](rows []N, seen map[sqlvalue.Value]bool, read func(N) (sqlvalue.Value, bool, error)) (map[sqlvalue.Value][]N, error) {
	groups := make(map[sqlvalue.Value][]N, len(seen))
	// The initial capacity across groups cannot exceed the returned row count.
	capacity := 1
	if len(seen) > 0 {
		capacity = max(1, len(rows)/len(seen))
	}
	for _, row := range rows {
		key, present, err := read(row)
		if err != nil {
			return nil, err
		}
		if !present || !seen[key] {
			return nil, safeerror.NewFault(fault.Invalid, "related row key does not match requested keys; check key equality semantics")
		}
		group, exists := groups[key]
		if !exists {
			group = make([]N, 0, capacity)
		}
		groups[key] = append(group, row)
	}
	return groups, nil
}
