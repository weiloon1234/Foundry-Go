package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// WriteHooks bridges generated model drafts and change sets into the shared
// write pipeline. Applications use their generated <Model>Hooks declaration.
// Both callbacks run inside the owning transaction, with all rows closed.
type WriteHooks[M any] struct {
	Before func(context.Context, *database.Tx, lifecycle.Operation, value.Optional[M], Mutation[M]) (Mutation[M], error)
	After  func(context.Context, *database.Tx, lifecycle.Operation, value.Optional[M], value.Optional[M], Mutation[M]) error
}

// WithWriteHooks attaches a factory at the generated declaration boundary.
// The factory runs once per normal write, after locking an existing row, inside
// the transaction. It does not run on reads, bulk inserts or SQL upserts.
func (d Definition[M]) WithWriteHooks(factory func() WriteHooks[M]) Definition[M] {
	d.writeHooks, d.hasWriteHooks, d.hasObserverHooks = nil, true, false
	if factory != nil {
		d.writeHooks = func(context.Context, lifecycle.Observers) (WriteHooks[M], error) { return factory(), nil }
	}
	return d
}

// WithObserverHooks attaches the generated, stage-composing adapter. local
// reports whether the model declares its own factory; otherwise the adapter
// runs only when its owning database has observers for M. It replaces a prior
// hook adapter. Factories run after row locking, once inside a normal write.
func (d Definition[M]) WithObserverHooks(factory func(context.Context, lifecycle.Observers) (WriteHooks[M], error), local bool) Definition[M] {
	d.writeHooks, d.hasWriteHooks, d.hasObserverHooks = factory, local, true
	return d
}

// observerOwner is the module-sealed capability implemented by DB,
// PrimaryExecutor, Session and Tx. Only these owners can prove observer absence
// without entering a transaction; an arbitrary wrapper must use the Tx it
// supplies. The capability reads a frozen snapshot without pool locks or I/O.
type observerOwner interface {
	FoundryObservers(sqlowner.Seal) (lifecycle.Observers, bool)
}

func writerObservers(writer any) (lifecycle.Observers, bool) {
	owner, ok := writer.(observerOwner)
	if !ok {
		return lifecycle.Observers{}, false
	}
	return owner.FoundryObservers(sqlowner.Seal{})
}

// MaxLifecycleDepth bounds nested write and retrieval callbacks using the
// supplied context. Hooks must propagate it to nested operations. Failures roll
// back owning writes/savepoints; pool reads have no implicit write transaction.
const MaxLifecycleDepth = 32

type lifecycleDepthKey struct{}

func writeHookContext(ctx context.Context) (context.Context, error) {
	depth, _ := ctx.Value(lifecycleDepthKey{}).(int)
	if depth >= MaxLifecycleDepth {
		return nil, fault.New(fault.Invalid, "model lifecycle nesting exceeds its depth bound; check recursive hooks")
	}
	return context.WithValue(ctx, lifecycleDepthKey{}, depth+1), nil
}

func executeHookedMutation[M any](ctx context.Context, writer database.Transactor, plan mutationPlan[M]) (M, error) {
	c, err := plan.query.mutationCompiler(plan.kind)
	if err != nil {
		return *new(M), err
	}
	if _, err := plan.query.validateMutationShape(plan.kind, plan.mutation, &c); err != nil {
		return *new(M), err
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (M, error) {
		var observers lifecycle.Observers
		if tx != nil {
			observers = tx.Observers()
		}
		registered := observedBy[M](observers, plan.kind)
		if registered && !plan.query.definition.hasObserverHooks {
			return *new(M), fault.New(fault.Invalid, "registered model observers require a generated observer adapter; regenerate the model")
		}
		if !registered && !plan.query.definition.hasWriteHooks {
			statement, err := prepareMutation(ctx, &plan, transactionClock(tx), transactionKeys(tx))
			if err != nil {
				return *new(M), err
			}
			return plan.returning(ctx, tx, statement)
		}
		ctx, err := writeHookContext(ctx)
		if err != nil {
			return *new(M), err
		}
		var before value.Optional[M]
		if plan.kind != insertModel {
			// A declaration is not proof of a physical unique constraint.
			// Bound the read and reject ambiguous rows before invoking hooks.
			statement, err := plan.query.ForUpdate().Limit(2).Compile()
			if err != nil {
				return *new(M), err
			}
			current, err := returningOne(ctx, tx, statement, plan.query.definition.scan)
			if err != nil {
				return *new(M), err
			}
			before = value.Set(current)
		}
		if err := ctx.Err(); err != nil {
			return *new(M), err
		}
		hooks, err := plan.query.definition.writeHooks(ctx, observersFor(observers, plan.kind))
		if err != nil {
			return *new(M), err
		}
		operation := plan.kind.operation()
		if hooks.Before != nil {
			mutation, err := hooks.Before(ctx, tx, operation, before, plan.mutation)
			if err != nil {
				return *new(M), err
			}
			plan.mutation = mutation
		}
		if err := ctx.Err(); err != nil {
			return *new(M), err
		}
		statement, err := prepareMutation(ctx, &plan, transactionClock(tx), transactionKeys(tx))
		if err != nil {
			return *new(M), err
		}
		result, err := plan.returning(ctx, tx, statement)
		if err != nil {
			return *new(M), err
		}
		after := value.Set(result)
		if plan.kind.sqlKind() == deleteModel {
			after = value.Optional[M]{}
		}
		if hooks.After != nil {
			if err := hooks.After(ctx, tx, operation, before, after, plan.mutation); err != nil {
				return *new(M), err
			}
		}
		if err := ctx.Err(); err != nil {
			return *new(M), err
		}
		return result, nil
	})
}
