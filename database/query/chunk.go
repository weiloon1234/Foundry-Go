package query

import (
	"context"
	"database/sql/driver"
	"math"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxChunkSize shares the framework's bounded model collection size.
const MaxChunkSize = MaxPageSize

// DefaultChunkSize is the batch size used by Each with eager relations or
// possible retrieval callbacks. Use EachChunked to choose an explicit size.
const DefaultChunkSize = 100

// Chunk processes complete model slices sequentially. It preserves filters,
// ordering and the selected Limit/Offset window, appending the primary key as
// a tie-breaker. Batches after the first advance by keyset on the ordered
// columns and key, so rows inserted or deleted behind the traversal do not
// shift later batches. Only orders that cannot be keyed (computed expressions,
// explicit NULLS FIRST/LAST or fields without generated getters) fall back to
// LIMIT/OFFSET. Rows and eager loads close before callbacks. Callback errors
// stop iteration; panics propagate, as with Each.
func (q Query[M]) Chunk(ctx context.Context, executor database.Executor, size int, yield func([]M) error) error {
	return q.chunks(ctx, executor, size, false, yield)
}

// ChunkByID advances using the declared primary key, including natural keys.
// Only key ordering is accepted (ascending by default, or explicit descending);
// nonzero Offset is rejected and Limit caps the total number of delivered models.
// The next key is captured before the callback can mutate its batch. Keep stored
// keys stable; this traversal does not create a snapshot across batches.
func (q Query[M]) ChunkByID(ctx context.Context, executor database.Executor, size int, yield func([]M) error) error {
	return q.chunks(ctx, executor, size, true, yield)
}

// EachChunked yields models individually from complete batches, using the same
// keyset/offset selection as Chunk. No database rows remain open during
// callbacks, even without eager relations.
func (q Query[M]) EachChunked(ctx context.Context, executor database.Executor, size int, yield func(M) error) error {
	if yield == nil {
		return fault.New(fault.Invalid, "model iteration requires a callback")
	}
	return q.Chunk(ctx, executor, size, eachBatch(ctx, yield))
}

// EachByID yields individual models from primary-key batches. It follows the
// ordering, window, callback and consistency contracts of ChunkByID.
func (q Query[M]) EachByID(ctx context.Context, executor database.Executor, size int, yield func(M) error) error {
	if yield == nil {
		return fault.New(fault.Invalid, "model iteration requires a callback")
	}
	return q.ChunkByID(ctx, executor, size, eachBatch(ctx, yield))
}

func eachBatch[M any](ctx context.Context, yield func(M) error) func([]M) error {
	return func(items []M) error {
		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := yield(item); err != nil {
				return err
			}
		}
		return nil
	}
}

// chunkPlan advances by keyset when keys holds one generated getter per order;
// otherwise it advances by offset.
type chunkPlan[M any] struct {
	base         Query[M]
	remaining    value.Optional[int]
	keys         []ModelField[M]
	after        []driver.Value
	size, offset int
	byID         bool
}

func (q Query[M]) chunkPlan(size int, byID bool) (chunkPlan[M], error) {
	if !validPageSize(size) {
		return chunkPlan[M]{}, fault.New(fault.Invalid, "invalid model chunk size")
	}
	q, err := q.stableModelOrder()
	if err != nil {
		return chunkPlan[M]{}, err
	}
	p := chunkPlan[M]{base: q, remaining: q.limit, size: size, offset: q.offset, byID: byID}
	if byID && (q.offset != 0 || len(q.orders) != 1 || q.orders[0].computed != nil || q.orders[0].nulls != nullsDefault || q.orders[0].field.column != q.definition.primary) {
		return chunkPlan[M]{}, fault.New(fault.Invalid, "primary-key chunks require key ordering and no Offset")
	}
	p.keys = q.keysetFields()
	if byID && p.keys == nil {
		return chunkPlan[M]{}, fault.New(fault.Invalid, "primary-key chunks require a generated key codec and getter")
	}
	p.base.limit = value.Optional[int]{}
	// Validate even an empty selected window before promising successful iteration.
	if _, err := p.window().Compile(); err != nil {
		return chunkPlan[M]{}, err
	}
	return p, nil
}

// keysetFields returns one generated getter per order when every order is a
// plain model field with default NULL placement. Nullable keys use the shared
// NULL-aware cursor predicate. Nil means the ordering cannot be keyed.
func (q Query[M]) keysetFields() []ModelField[M] {
	if q.definition == nil || len(q.orders) == 0 || len(q.orders) > MaxCursorFields {
		return nil
	}
	fields := make([]ModelField[M], len(q.orders))
	for i, order := range q.orders {
		if order.computed != nil || order.nulls != nullsDefault || order.field.table != q.table {
			return nil
		}
		field, ok := q.definition.modelField(order.field.column)
		if !ok || field.get == nil {
			return nil
		}
		fields[i] = field
	}
	return fields
}

func (p chunkPlan[M]) take() int {
	if remaining, bounded := p.remaining.Get(); bounded && remaining < p.size {
		return remaining
	}
	return p.size
}

func (p chunkPlan[M]) window() Query[M] {
	q := p.base.Limit(p.take()).Offset(p.offset)
	if p.after != nil {
		q = q.Where(cursorPredicate(q.orders, p.after, q.definition.nullableOrder))
	}
	return q
}

// advance owns progress independently of callback edits to models or slices.
// Replacing the boundary on the immutable base prevents predicate accumulation.
func (p *chunkPlan[M]) advance(items []M) (bool, error) {
	count, requested := len(items), p.take()
	if count > requested {
		return false, fault.New(fault.Invalid, "database exceeded the model chunk limit")
	}
	if remaining, bounded := p.remaining.Get(); bounded {
		p.remaining = value.Set(remaining - count)
	}
	if count < requested || p.take() == 0 {
		return false, nil
	}
	if p.keys == nil {
		if p.offset > math.MaxInt-count {
			return false, fault.New(fault.Invalid, "model chunk offset overflow")
		}
		p.offset += count
		return true, nil
	}
	last := items[count-1]
	after := make([]driver.Value, len(p.keys))
	for i, field := range p.keys {
		key, err := field.get(last)
		if err != nil {
			return false, err
		}
		if key == nil && field.column == p.base.definition.primary {
			return false, fault.New(fault.Invalid, "model chunk primary key cannot be NULL")
		}
		after[i] = key
	}
	// The first window may start at Offset; later windows start after the
	// last delivered key.
	p.after, p.offset = after, 0
	return true, nil
}

func (q Query[M]) chunks(ctx context.Context, executor database.Executor, size int, byID bool, yield func([]M) error) error {
	if err := executionContext(ctx, executor); err != nil {
		return err
	}
	q = q.inContext(ctx)
	if yield == nil {
		return fault.New(fault.Invalid, "model chunk iteration requires a callback")
	}
	p, err := q.chunkPlan(size, byID)
	if err != nil {
		return err
	}
	for p.take() != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := p.window().All(ctx, executor)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		more, err := p.advance(items)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := yield(items); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	return nil
}
