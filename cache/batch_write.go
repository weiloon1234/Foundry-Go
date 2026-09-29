package cache

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Entry is one typed key/value pair written by PutMany.
type Entry[K, V any] struct {
	Key   K
	Value V
}

// PutMany writes up to Config.MaxBatchEntries entries with one TTL. Every key
// and value is encoded before the first write, so a codec failure writes
// nothing. Entries then use one tag snapshot (adapters with SnapshotWriteBackend
// resolve the current snapshot inside each write). The batch is not atomic:
// entries are written in input order (a repeated key keeps its last value), the
// first failure stops the remaining writes, and earlier entries stay stored. No
// write is retried; Config.Timeout bounds the whole batch.
func (c Cache[K, V]) PutMany(ctx context.Context, ttl TTL, entries ...Entry[K, V]) error {
	if err := ttl.Validate(); err != nil {
		return err
	}
	started := c.startedAt()
	var written uint64
	err := c.operation(ctx, func(ctx context.Context) error {
		if len(entries) > c.store.config.MaxBatchEntries {
			return fault.New(fault.Invalid, "cache batch exceeds configured key limit")
		}
		if len(entries) == 0 {
			return nil
		}
		addresses := make([]EntryKey, len(entries))
		payloads := make([][]byte, len(entries))
		for i, entry := range entries {
			var err error
			if addresses[i], err = c.address(entry.Key); err != nil {
				return err
			}
			if payloads[i], err = c.encode(ctx, entry.Value, ttl); err != nil {
				return err
			}
		}
		accesses, err := c.writeAccesses(ctx, addresses)
		if err != nil {
			return err
		}
		defer forgetMemo(ctx, c.store, addresses...)
		for i, access := range accesses {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := access.backend.Put(ctx, access.key, payloads[i], ttl); err != nil {
				return err
			}
			written++
		}
		return nil
	})
	c.store.counters.writes.Add(written)
	c.report(ctx, Event{Operation: OperationPutMany}, started, err)
	return err
}

// writeAccesses binds direct mutations of several addresses to one resolved
// snapshot, or to per-write resolution for SnapshotWriteBackend adapters.
func (c Cache[K, V]) writeAccesses(ctx context.Context, bases []EntryKey) ([]entryAccess, error) {
	accesses := make([]entryAccess, len(bases))
	if _, ok := c.store.backend.(SnapshotWriteBackend); ok && c.scoped() {
		for i, base := range bases {
			var err error
			if accesses[i], err = c.writeAccess(ctx, base); err != nil {
				return nil, err
			}
		}
		return accesses, nil
	}
	snapshot, err := c.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	for i, base := range bases {
		if accesses[i], err = snapshot.bind(base); err != nil {
			return nil, err
		}
	}
	return accesses, nil
}
