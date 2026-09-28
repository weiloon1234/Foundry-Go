package query

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// RetrievalHooks bridges concrete generated read callbacks into the shared read
// pipeline. Retrieved observes a stored model; it does not replace its fields or
// evaluate getters. The executor is the same capability supplied to the read,
// including any application wrapper. It is not implicitly a write transaction.
type RetrievalHooks[M any] struct {
	Retrieved func(context.Context, database.Executor, M) error
}

// WithRetrievalHooks installs a generated adapter without constructing hooks.
// local reports a model-local factory. Factories run after a nonempty complete
// fetch has hydrated and closed its rows, once per fetch/batch. Read failures
// discard pending results; reads on a pool do not create an implicit transaction.
func (d Definition[M]) WithRetrievalHooks(factory func(context.Context, lifecycle.Observers) (RetrievalHooks[M], error), local bool) Definition[M] {
	d.readHooks, d.hasReadHooks, d.hasReadAdapter = factory, local, true
	return d
}

// The private callback adapter is carried only by complete model records. A DTO
// projection or scalar has no adapter, even if its Go result type is also used by
// a model declaration. The ordered decoder remains the source of stored values.
type readLifecycle[R any] struct {
	primaryIndex int
	needed       func(lifecycle.Observers) bool
	prepare      func(context.Context, lifecycle.Observers) (func(context.Context, database.Executor, R) error, error)
}

func (d *Definition[M]) retrieval() *readLifecycle[M] {
	index := -1
	for i, column := range d.columns {
		if column.Name == d.primary {
			index = i
			break
		}
	}
	return &readLifecycle[M]{
		primaryIndex: index,
		needed: func(set lifecycle.Observers) bool {
			return d.hasReadHooks || lifecycle.HasRetrievalObservers[M](set)
		},
		prepare: func(ctx context.Context, set lifecycle.Observers) (func(context.Context, database.Executor, M) error, error) {
			if d.readHooks == nil {
				return nil, fault.New(fault.Invalid, "registered model retrieval observers require a generated adapter; regenerate the model")
			}
			hooks, err := d.readHooks(ctx, set)
			return hooks.Retrieved, err
		},
	}
}

// A known concrete owner can prove absence before SQL. Wrappers must preserve
// their actual returned Rows ownership; unknown ownership cannot skip callbacks.
func (r *readLifecycle[R]) mayRun(executor database.Executor) bool {
	if r == nil {
		return false
	}
	writer, ok := executor.(database.Transactor)
	if !ok {
		return true
	}
	set, known := writerObservers(writer)
	return !known || r.needed(set)
}

// Through reads hydrate both target and pivot models from the same closed row
// batch. Each selected role keeps its exact model-owned adapter and factory.
func joinedReadLifecycle[N, P any](target *Definition[N], pivot *Definition[P]) *readLifecycle[relation.Link[N, P]] {
	n, p := target.retrieval(), pivot.retrieval()
	return &readLifecycle[relation.Link[N, P]]{
		primaryIndex: -1,
		needed:       func(set lifecycle.Observers) bool { return n.needed(set) || p.needed(set) },
		prepare: func(ctx context.Context, set lifecycle.Observers) (func(context.Context, database.Executor, relation.Link[N, P]) error, error) {
			var targetCallback func(context.Context, database.Executor, N) error
			var pivotCallback func(context.Context, database.Executor, P) error
			var err error
			if n.needed(set) {
				targetCallback, err = n.prepare(ctx, set)
				if err != nil {
					return nil, err
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if p.needed(set) {
				pivotCallback, err = p.prepare(ctx, set)
				if err != nil {
					return nil, err
				}
			}
			return func(ctx context.Context, executor database.Executor, item relation.Link[N, P]) error {
				if targetCallback != nil {
					if err := targetCallback(ctx, executor, item.Model); err != nil {
						return err
					}
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if pivotCallback != nil {
					return pivotCallback(ctx, executor, item.Pivot)
				}
				return nil
			}, nil
		},
	}
}

// collectRead shares one raw execution/decoder path. It retains callback work
// before EOF closes the stream, then constructs/dispatches hooks with no rows
// open. The entire partial result is discarded on scan, close or hook failure.
func collectRead[R any](ctx context.Context, executor database.Executor, statement Statement, scan func(database.Row) (R, error), callbacks *readLifecycle[R]) (result []R, err error) {
	rows, err := database.ReadQuery(ctx, executor, statement.sql, statement.arguments...)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, rows.Close())
		if err != nil {
			result = nil
		}
	}()
	result = make([]R, 0)
	collect := func(ctx context.Context) error {
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			item, err := scan(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return errors.Join(rows.Close(), ctx.Err())
	}
	set := rows.Observers()
	if callbacks == nil || !callbacks.needed(set) {
		err = collect(ctx)
		return result, err
	}
	err = rows.WithObserverScope(ctx, func(work context.Context) error {
		if err := collect(work); err != nil || len(result) == 0 {
			return err
		}
		work, err := writeHookContext(work)
		if err != nil {
			return err
		}
		invoke, err := callbacks.prepare(work, set)
		if err != nil {
			return err
		}
		for _, item := range result {
			if err := work.Err(); err != nil {
				return err
			}
			if invoke != nil {
				if err := invoke(work, executor, item); err != nil {
					return err
				}
			}
		}
		return work.Err()
	})
	return result, err
}
