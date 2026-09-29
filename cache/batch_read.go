package cache

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Lookup is one typed batch-read result. Found distinguishes a miss from a
// stored zero value.
type Lookup[V any] struct {
	Value V
	Found bool
}

// GetMany reads up to Config.MaxBatchEntries keys under one tag snapshot and
// returns results in input order (a repeated key repeats its result). Adapters
// implementing BatchReadBackend/TaggedBatchReadBackend (memory and Redis) read the
// whole batch in one operation; others read each distinct key in turn. Like Get,
// a snapshot replaced by a concurrent invalidation is re-resolved (bounded) and
// otherwise every key is a miss. Each value is decoded independently.
func (c Cache[K, V]) GetMany(ctx context.Context, keys ...K) ([]Lookup[V], error) {
	started := c.startedAt()
	results := make([]Lookup[V], len(keys))
	err := c.operation(ctx, func(ctx context.Context) error {
		if len(keys) > c.store.config.MaxBatchEntries {
			return fault.New(fault.Invalid, "cache batch exceeds configured key limit")
		}
		if len(keys) == 0 {
			return nil
		}
		addresses := make([]EntryKey, len(keys))
		for i, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}
			var err error
			if addresses[i], err = c.address(key); err != nil {
				return err
			}
		}
		unique := slices.Clone(addresses)
		slices.SortFunc(unique, func(a, b EntryKey) int { return strings.Compare(a.String(), b.String()) })
		unique = slices.Compact(unique)
		values, err := c.readMemoizedBatch(ctx, unique)
		if err != nil {
			return err
		}
		for i, address := range addresses {
			index, _ := slices.BinarySearchFunc(unique, address, func(a, b EntryKey) int { return strings.Compare(a.String(), b.String()) })
			stored := values[index]
			if !stored.Found || len(stored.Data) > c.store.config.MaxValueBytes {
				continue
			}
			// Duplicated keys share stored bytes; each result decodes its own copy.
			value, err := c.decode(ctx, slices.Clone(stored.Data))
			if err != nil {
				return err
			}
			results[i] = Lookup[V]{Value: value, Found: true}
		}
		return nil
	})
	if err != nil {
		c.report(ctx, Event{Operation: OperationGetMany}, started, err)
		return nil, err
	}
	event := Event{Operation: OperationGetMany}
	for _, result := range results {
		c.store.countRead(result.Found)
		if result.Found {
			event.Hits++
		} else {
			event.Misses++
		}
	}
	c.report(ctx, event, started, nil)
	return results, nil
}

// readMemoizedBatch answers memoized addresses from ctx's memo and reads the
// rest in one batch, memoizing what it read.
func (c Cache[K, V]) readMemoizedBatch(ctx context.Context, bases []EntryKey) ([]BatchValue, error) {
	memo, err := c.memoView(ctx)
	if err != nil {
		return nil, err
	}
	if memo.memo == nil {
		return c.readBatch(ctx, bases)
	}
	values := make([]BatchValue, len(bases))
	pending := make([]EntryKey, 0, len(bases))
	positions := make([]int, 0, len(bases))
	for i, base := range bases {
		if entry, ok := memo.load(base); ok {
			values[i] = BatchValue{Data: entry.data, Found: entry.found}
			continue
		}
		pending, positions = append(pending, base), append(positions, i)
	}
	if len(pending) == 0 {
		return values, nil
	}
	read, err := c.readBatch(ctx, pending)
	if err != nil {
		return nil, err
	}
	for j, i := range positions {
		value := read[j]
		if len(value.Data) > c.store.config.MaxValueBytes {
			value = BatchValue{}
		}
		memo.save(pending[j], value.Data, value.Found)
		values[i] = value
	}
	return values, nil
}

// readBatch reads canonical unique addresses under one snapshot.
func (c Cache[K, V]) readBatch(ctx context.Context, bases []EntryKey) ([]BatchValue, error) {
	values, _, err := c.readBatchSnapshot(ctx, bases)
	return values, err
}

// readBatchSnapshot also returns the snapshot the batch was read under. A
// snapshot replaced by concurrent invalidations is re-resolved (bounded) and
// otherwise every key is a miss under the last snapshot.
func (c Cache[K, V]) readBatchSnapshot(ctx context.Context, bases []EntryKey) ([]BatchValue, accessSnapshot, error) {
	for attempt := 1; ; attempt++ {
		snapshot, err := c.snapshot(ctx)
		if err != nil {
			return nil, accessSnapshot{}, err
		}
		values, err := c.readSnapshotBatch(ctx, snapshot, bases)
		if err == nil || snapshot.tagged == nil || !errorgraph.Is(err, fault.Conflict) {
			return values, snapshot, err
		}
		if err := ctx.Err(); err != nil {
			return nil, accessSnapshot{}, err
		}
		if attempt == snapshotAttempts {
			return make([]BatchValue, len(bases)), snapshot, nil
		}
		c.store.counters.snapshotRetries.Add(1)
	}
}
func (c Cache[K, V]) readSnapshotBatch(ctx context.Context, snapshot accessSnapshot, bases []EntryKey) ([]BatchValue, error) {
	var values []BatchValue
	var err error
	if snapshot.tagged == nil {
		if backend, ok := c.store.backend.(BatchReadBackend); ok {
			values, err = backend.GetMany(ctx, bases)
		} else {
			values = make([]BatchValue, len(bases))
			for i, base := range bases {
				if values[i].Data, values[i].Found, err = c.read(ctx, entryAccess{key: base, fillKey: base, backend: snapshot.backend}); err != nil {
					return nil, err
				}
			}
		}
	} else {
		batch := make([]TaggedKey, len(bases))
		for i, base := range bases {
			if batch[i], err = NewTaggedKey(base, snapshot.stamps); err != nil {
				return nil, err
			}
		}
		if backend, ok := c.store.backend.(TaggedBatchReadBackend); ok {
			// Tagged data addresses differ from bases; keep input order aligned.
			order := make([]int, len(batch))
			for i := range order {
				order[i] = i
			}
			slices.SortFunc(order, func(a, b int) int { return strings.Compare(batch[a].DataKey().String(), batch[b].DataKey().String()) })
			sorted := make([]TaggedKey, len(batch))
			for i, index := range order {
				sorted[i] = batch[index]
			}
			read, readErr := backend.GetManyTagged(ctx, sorted)
			if readErr != nil {
				return nil, readErr
			}
			if len(read) != len(sorted) {
				return nil, fault.New(fault.Invalid, "cache backend returned an invalid batch")
			}
			values = make([]BatchValue, len(batch))
			for i, index := range order {
				values[index] = read[i]
			}
		} else {
			values = make([]BatchValue, len(batch))
			for i, key := range batch {
				if values[i].Data, values[i].Found, err = c.read(ctx, taggedEntryAccess(snapshot.tagged, key)); err != nil {
					return nil, err
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(values) != len(bases) {
		return nil, fault.New(fault.Invalid, "cache backend returned an invalid batch")
	}
	return values, ctx.Err()
}

// Pull reads and removes one entry. It is not atomic: a concurrent writer can
// replace the value between the read and the removal, which then removes the
// replacement. Use it for single-consumer values such as one-time notices.
func (c Cache[K, V]) Pull(ctx context.Context, key K) (V, bool, error) {
	value, found, err := c.Get(ctx, key)
	if err != nil || !found {
		return value, found, err
	}
	if _, err := c.Forget(ctx, key); err != nil {
		return *new(V), false, err
	}
	return value, true, nil
}
