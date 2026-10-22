package orm

import (
	"context"
	"database/sql/driver"
	"math"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// MaxChunkSize is the existing bound on one model batch.
const MaxChunkSize = query.MaxChunkSize

// Chunk reads stable, bounded batches, closing rows before each callback.
// It preserves the query window and advances by keyset when ordering permits;
// explicit NULL placement falls back to offsets. It does not create a snapshot.
func (q Query[M]) Chunk(ctx context.Context, ex database.Executor, size int, yield func([]M) error) error {
	return chunks(ctx, ex, q.state, size, yield)
}

// EachChunked yields individual models from fully read and closed batches.
func (q Query[M]) EachChunked(ctx context.Context, ex database.Executor, size int, yield func(M) error) error {
	if yield == nil {
		return invalid("model iteration requires a callback")
	}
	return chunks(ctx, ex, q.state, size, eachBatch(ctx, yield))
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

// Progress is independent of M and captured before callbacks can edit a batch.
type chunkPlan struct {
	base         queryState
	remaining    value.Optional[int]
	keys         []*columnCore
	after        []driver.Value
	size, offset int
}

func newChunkPlan(q queryState, size int) (chunkPlan, error) {
	if size < 1 || size > MaxChunkSize {
		return chunkPlan{}, invalid("invalid model chunk size")
	}
	if err := validateQuery(q); err != nil {
		return chunkPlan{}, err
	}
	var err error
	q.ast, err = q.ast.StableOrder()
	if err != nil {
		return chunkPlan{}, err
	}
	p := chunkPlan{base: q, remaining: q.ast.Limit, size: size, offset: q.ast.Offset, keys: keysetFields(q)}
	p.base.ast.Limit = value.Optional[int]{}
	// Even a zero window validates predicates and bindings before succeeding.
	if _, err := compileRead(p.window(), querycore.ReadModels); err != nil {
		return chunkPlan{}, err
	}
	return p, nil
}

func keysetFields(q queryState) []*columnCore {
	if len(q.ast.Orders) == 0 || len(q.ast.Orders) > querycore.MaxCursorFields {
		return nil
	}
	keys := make([]*columnCore, len(q.ast.Orders))
	for i, order := range q.ast.Orders {
		if order.Computed != nil || order.Nulls != querycore.NullsDefault || order.Field.Table != q.ast.Table {
			return nil
		}
		for _, column := range q.table.columns {
			if column.metadata.Name == order.Field.Column && column.get != nil {
				keys[i] = column
				break
			}
		}
		if keys[i] == nil {
			return nil
		}
	}
	return keys
}
func (p chunkPlan) take() int {
	if remaining, bounded := p.remaining.Get(); bounded && remaining < p.size {
		return remaining
	}
	return p.size
}
func (p chunkPlan) window() queryState {
	q := p.base
	q.ast.Limit = value.Set(p.take())
	q.ast.Offset = p.offset
	if p.after != nil {
		q.ast.Predicates = append(slices.Clone(q.ast.Predicates), querycore.CursorPredicate(q.ast.Orders, p.after, q.table.model.NullableOrder))
	}
	return q
}
func (p *chunkPlan) advance(count int, last unsafe.Pointer) (bool, error) {
	requested := p.take()
	if count > requested {
		return false, invalid("database exceeded the model chunk limit")
	}
	if remaining, bounded := p.remaining.Get(); bounded {
		p.remaining = value.Set(remaining - count)
	}
	if count < requested || p.take() == 0 {
		return false, nil
	}
	if p.keys == nil {
		if p.offset > math.MaxInt-count {
			return false, invalid("model chunk offset overflow")
		}
		p.offset += count
		return true, nil
	}
	after := make([]driver.Value, len(p.keys))
	for i, column := range p.keys {
		key, err := column.get(last)
		if err != nil {
			return false, err
		}
		if key == nil && column.metadata.Name == p.base.table.model.Primary {
			return false, invalid("model chunk primary key cannot be NULL")
		}
		after[i] = key
	}
	p.after, p.offset = after, 0
	return true, nil
}
func chunks[M any](ctx context.Context, ex database.Executor, q queryState, size int, yield func([]M) error) error {
	if err := executionContext(ctx, ex); err != nil {
		return err
	}
	q.ast.ScopeContext = ctx
	if yield == nil {
		return invalid("model chunk iteration requires a callback")
	}
	p, err := newChunkPlan(q, size)
	if err != nil {
		return err
	}
	for p.take() != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := all[M](ctx, ex, p.window())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		more, err := p.advance(len(items), unsafe.Pointer(&items[len(items)-1]))
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
