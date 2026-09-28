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

// Chunk processes complete model slices sequentially using LIMIT/OFFSET. It
// preserves filters, ordering and the selected Limit/Offset window, appending
// the primary key as a tie-breaker. Rows and eager loads close before callbacks.
// Callback errors stop iteration; panics propagate, as with Each.
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

// EachChunked yields models individually from complete offset-based batches.
// No database rows remain open during callbacks, even without eager relations.
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

type chunkPlan[M any] struct {
	base         Query[M]
	remaining    value.Optional[int]
	key          ModelField[M]
	after        driver.Value
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
	if byID {
		if q.offset != 0 || len(q.orders) != 1 || q.orders[0].computed != nil || q.orders[0].field.column != q.definition.primary {
			return chunkPlan[M]{}, fault.New(fault.Invalid, "primary-key chunks require key ordering and no Offset")
		}
		var exists bool
		p.key, exists = q.definition.modelField(q.definition.primary)
		if !exists {
			return chunkPlan[M]{}, fault.New(fault.Invalid, "primary-key chunks require a generated key codec and getter")
		}
	}
	p.base.limit = value.Optional[int]{}
	// Validate even an empty selected window before promising successful iteration.
	if _, err := p.window().Compile(); err != nil {
		return chunkPlan[M]{}, err
	}
	return p, nil
}

func (p chunkPlan[M]) take() int {
	if remaining, bounded := p.remaining.Get(); bounded && remaining < p.size {
		return remaining
	}
	return p.size
}

func (p chunkPlan[M]) window() Query[M] {
	q := p.base.Limit(p.take()).Offset(p.offset)
	if p.byID && p.after != nil {
		q = q.Where(cursorPredicate(q.orders, []driver.Value{p.after}))
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
	if p.byID {
		key, err := p.key.get(items[count-1])
		if err != nil {
			return false, err
		}
		if key == nil {
			return false, fault.New(fault.Invalid, "model chunk primary key cannot be NULL")
		}
		p.after = key
	} else {
		if p.offset > math.MaxInt-count {
			return false, fault.New(fault.Invalid, "model chunk offset overflow")
		}
		p.offset += count
	}
	return true, nil
}

func (q Query[M]) chunks(ctx context.Context, executor database.Executor, size int, byID bool, yield func([]M) error) error {
	if err := executionContext(ctx, executor); err != nil {
		return err
	}
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
