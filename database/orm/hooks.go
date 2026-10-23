package orm

import (
	"context"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Hooks declares operation-local write callbacks; nil callbacks are skipped.
// Example: Hooks[Order]{Creating: func(ctx context.Context, tx *database.Tx, w *Write[Order]) error { return WriteSet(w, Orders.TotalCents.Set(0)) }}.
type Hooks[M any] struct {
	// Saving precedes Creating/Updating. Example: Saving: validateOrder.
	Saving func(context.Context, *database.Tx, value.Optional[M], *Write[M]) error
	// Creating can supply omitted required fields. Example: Creating: initializeOrder.
	Creating func(context.Context, *database.Tx, *Write[M]) error
	// Updating receives the locked original. Example: Updating: reviseOrder.
	Updating func(context.Context, *database.Tx, M, *Write[M]) error
	// Deleting precedes soft and physical deletion. Example: Deleting: authorizeRemoval.
	Deleting func(context.Context, *database.Tx, M) error
	// Restoring precedes restoring a deleted row. Example: Restoring: authorizeRestore.
	Restoring func(context.Context, *database.Tx, M) error
	// ForceDeleting precedes Deleting. Example: ForceDeleting: authorizePurge.
	ForceDeleting func(context.Context, *database.Tx, M) error
	// Created runs after RETURNING, inside the transaction. Example: Created: recordCreation.
	Created func(context.Context, *database.Tx, Changes[M]) error
	// Updated runs after RETURNING, inside the transaction. Example: Updated: recordRevision.
	Updated func(context.Context, *database.Tx, Changes[M]) error
	// Deleted runs after soft or physical deletion. Example: Deleted: recordRemoval.
	Deleted func(context.Context, *database.Tx, Changes[M]) error
	// Saved follows Created/Updated. Example: Saved: writeOutbox.
	Saved func(context.Context, *database.Tx, Changes[M]) error
	// Restored runs after restoration. Example: Restored: recordRestore.
	Restored func(context.Context, *database.Tx, Changes[M]) error
	// ForceDeleted follows Deleted. Example: ForceDeleted: recordPurge.
	ForceDeleted func(context.Context, *database.Tx, Changes[M]) error
	// AfterCommit runs after the outer commit; it is not crash durable. Example: AfterCommit: invalidateCache.
	AfterCommit func(context.Context, lifecycle.Operation, Changes[M]) error
}

// Observer fixes registration to this model's Hooks type. Example: NewObserver[Order]("orders.audit").
type Observer[M any] struct {
	observer lifecycle.Observer[M, hookSet]
}

// Name returns the registration identifier. Example: observer.Name().
func (o Observer[M]) Name() string { return o.observer.Name() }

// Validate checks the immutable identifier without invoking its factory. Example: observer.Validate().
func (o Observer[M]) Validate() error { return o.observer.Validate() }

// Declare captures a typed per-operation factory. Example: NewObserver[Order]("orders.audit").Declare(orderAuditHooks).
func (o Observer[M]) Declare(factory func() Hooks[M]) (lifecycle.Declaration, error) {
	var wrapped func() hookSet
	if factory != nil {
		wrapped = func() hookSet { return eraseHooks(factory()) }
	}
	return o.observer.Declare(wrapped)
}

// NewObserver declares a write observer without invoking its factory. Example: NewObserver[Order]("orders.audit").
func NewObserver[M any](name string) Observer[M] {
	return Observer[M]{observer: lifecycle.NewObserver[M, hookSet](name)}
}

// NewDeletionObserver observes only delete/soft-delete/force-delete writes. Example: NewDeletionObserver[Order]("orders.removal").
func NewDeletionObserver[M any](name string) Observer[M] {
	return Observer[M]{observer: lifecycle.NewDeletionObserver[M, hookSet](name)}
}

// RegisterObserver installs instance-owned dependencies through a database Module.
// Example: RegisterObserver(r, pool, NewObserver[Order]("orders.audit"), constructOrderAudit).
func RegisterObserver[M any](r *foundation.Registrar, pool foundation.Key[*database.DB], observer Observer[M], construct func(foundation.Resolver) (func() Hooks[M], error)) error {
	if construct == nil {
		return invalid("model observer needs a dependency constructor")
	}
	return database.RegisterObserver(r, pool, observer.observer, func(resolver foundation.Resolver) (func() hookSet, error) {
		factory, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		if factory == nil {
			return nil, invalid("model observer needs a hook factory")
		}
		return func() hookSet { return eraseHooks(factory()) }, nil
	})
}

func ownerObservers(writer any) (lifecycle.Observers, bool) {
	return query.FoundryObservers(writer, sqlowner.Observers())
}
func observed[M any](set lifecycle.Observers, kind querycore.MutationKind) bool {
	if kind.Deletes() {
		return lifecycle.HasDeletionObservers[M](set)
	}
	return lifecycle.HasObservers[M](set)
}

type writeCallbacks struct {
	needed func(lifecycle.Observers, querycore.MutationKind) bool
	before func(context.Context, *database.Tx, *writePlan) error
	after  func(context.Context, *database.Tx, writePlan) error
}

// The erased stages retain typed adapters only on declarations that use hooks.
// Ordinary table methods reach this shared engine, not Hooks[M]'s callback graph.
type beforeStage uint8

const (
	savingStage beforeStage = iota
	creatingStage
	updatingStage
	deletingStage
	restoringStage
	forceDeletingStage
)

type afterStage uint8

const (
	createdStage afterStage = iota
	updatedStage
	deletedStage
	savedStage
	restoredStage
	forceDeletedStage
)

type hookSet struct {
	before [6]func(context.Context, *database.Tx, *tableCore, unsafe.Pointer, *draftState) error
	after  [6]func(context.Context, *database.Tx, *changeState) error
	commit func(context.Context, lifecycle.Operation, *changeState) error
}

func eraseHooks[M any](h Hooks[M]) hookSet {
	var set hookSet
	if h.Saving != nil {
		set.before[savingStage] = func(ctx context.Context, tx *database.Tx, t *tableCore, before unsafe.Pointer, d *draftState) error {
			return h.Saving(ctx, tx, optionalSnapshot[M](t, before), &Write[M]{state: d})
		}
	}
	if h.Creating != nil {
		set.before[creatingStage] = func(ctx context.Context, tx *database.Tx, t *tableCore, before unsafe.Pointer, d *draftState) error {
			return h.Creating(ctx, tx, &Write[M]{state: d})
		}
	}
	if h.Updating != nil {
		set.before[updatingStage] = func(ctx context.Context, tx *database.Tx, t *tableCore, before unsafe.Pointer, d *draftState) error {
			v, _ := optionalSnapshot[M](t, before).Get()
			return h.Updating(ctx, tx, v, &Write[M]{state: d})
		}
	}
	for _, pair := range []struct {
		stage beforeStage
		fn    func(context.Context, *database.Tx, M) error
	}{{deletingStage, h.Deleting}, {restoringStage, h.Restoring}, {forceDeletingStage, h.ForceDeleting}} {
		if pair.fn != nil {
			set.before[pair.stage] = func(ctx context.Context, tx *database.Tx, t *tableCore, before unsafe.Pointer, d *draftState) error {
				v, _ := optionalSnapshot[M](t, before).Get()
				return pair.fn(ctx, tx, v)
			}
		}
	}
	for _, pair := range []struct {
		stage afterStage
		fn    func(context.Context, *database.Tx, Changes[M]) error
	}{{createdStage, h.Created}, {updatedStage, h.Updated}, {deletedStage, h.Deleted}, {savedStage, h.Saved}, {restoredStage, h.Restored}, {forceDeletedStage, h.ForceDeleted}} {
		if pair.fn != nil {
			set.after[pair.stage] = func(ctx context.Context, tx *database.Tx, c *changeState) error {
				return pair.fn(ctx, tx, Changes[M]{state: c})
			}
		}
	}
	if h.AfterCommit != nil {
		set.commit = func(ctx context.Context, operation lifecycle.Operation, c *changeState) error {
			return h.AfterCommit(ctx, operation, Changes[M]{state: c})
		}
	}
	return set
}
func beforeStages(operation lifecycle.Operation) []beforeStage {
	switch operation {
	case lifecycle.Create:
		return []beforeStage{savingStage, creatingStage}
	case lifecycle.Update:
		return []beforeStage{savingStage, updatingStage}
	case lifecycle.Delete, lifecycle.SoftDelete:
		return []beforeStage{deletingStage}
	case lifecycle.Restore:
		return []beforeStage{restoringStage}
	case lifecycle.ForceDelete:
		return []beforeStage{forceDeletingStage, deletingStage}
	}
	return nil
}
func afterStages(operation lifecycle.Operation) []afterStage {
	switch operation {
	case lifecycle.Create:
		return []afterStage{createdStage, savedStage}
	case lifecycle.Update:
		return []afterStage{updatedStage, savedStage}
	case lifecycle.Delete, lifecycle.SoftDelete:
		return []afterStage{deletedStage}
	case lifecycle.Restore:
		return []afterStage{restoredStage}
	case lifecycle.ForceDelete:
		return []afterStage{deletedStage, forceDeletedStage}
	}
	return nil
}
func modelCallbacks(table *tableCore, candidate unsafe.Pointer) *writeCallbacks {
	var before unsafe.Pointer
	var hooks []hookSet
	var work context.Context
	return &writeCallbacks{
		needed: func(set lifecycle.Observers, kind querycore.MutationKind) bool {
			return table.hooks != nil || table.observed(set, kind)
		},
		before: func(ctx context.Context, tx *database.Tx, p *writePlan) error {
			set := tx.Observers()
			if table.hooks == nil && !table.observed(set, p.kind) {
				return nil
			}
			var err error
			work, err = lifecore.Context(ctx)
			if err != nil {
				return err
			}
			if p.kind != querycore.InsertModel {
				q := p.query
				q.ast.ScopeContext = work
				q.lock = querycore.LockUpdate
				q.ast.Limit = value.Set(2)
				statement, err := compileRead(q, querycore.ReadModels)
				if err != nil {
					return err
				}
				rows, err := tx.Query(statement.Context(work), statement.SQLText, statement.Bindings...)
				if err != nil {
					return err
				}
				original := reflect.New(table.typ).UnsafePointer()
				if err = returning(work, rows, func(row database.Row) error { return hydrate(table, row, original) }); err != nil {
					return err
				}
				before = snapshotPointer(table, original)
			}
			if p.kind.Deletes() {
				set = set.ForDeletion()
			}
			factories, err := table.observers(set)
			if err != nil {
				return err
			}
			if err = work.Err(); err != nil {
				return err
			}
			if table.hooks != nil {
				hooks = append(hooks, table.hooks())
			}
			for _, factory := range factories {
				if err = work.Err(); err != nil {
					return err
				}
				hooks = append(hooks, factory())
			}
			draft := &draftState{table: table, assignments: slices.Clone(p.assignments), open: true}
			defer func() { draft.open = false }()
			for _, stage := range beforeStages(p.kind.Operation()) {
				for _, h := range hooks {
					if err = work.Err(); err != nil {
						return err
					}
					if fn := h.before[stage]; fn != nil {
						if err = fn(work, tx, table, before, draft); err != nil {
							return lifecore.Failure("model before hook failed", err)
						}
					}
					if draft.err != nil {
						return draft.err
					}
				}
			}
			if err = work.Err(); err != nil {
				return err
			}
			p.assignments = draft.assignments
			p.query.ast.ScopeContext = work
			return nil
		},
		after: func(ctx context.Context, tx *database.Tx, p writePlan) error {
			if work == nil {
				return nil
			}
			after := candidate
			if p.kind.SqlKind() == querycore.DeleteModel {
				after = nil
			}
			assigned := p.assignments
			if p.adjust != nil {
				assigned = append(slices.Clone(assigned), querycore.MutationAssignment{Field: p.adjust.Field})
			}
			changes, err := captureChanges(table, before, after, assigned, value.Set(p.kind.Operation()))
			if err != nil {
				return err
			}
			for _, stage := range afterStages(p.kind.Operation()) {
				for _, h := range hooks {
					if err = work.Err(); err != nil {
						return err
					}
					if fn := h.after[stage]; fn != nil {
						if err = fn(work, tx, changes); err != nil {
							return lifecore.Failure("model after hook failed", err)
						}
					}
				}
			}
			if err = work.Err(); err != nil {
				return err
			}
			for _, h := range hooks {
				if h.commit != nil {
					if err = tx.AfterCommit(func(ctx context.Context) error {
						return lifecore.Failure("model after-commit hook failed", h.commit(ctx, p.kind.Operation(), changes))
					}); err != nil {
						return err
					}
				}
			}
			return work.Err()
		},
	}
}

// WithoutModelHooks acknowledges set writes skip per-model hooks, observers and changes.
// Per-model writes reject this flag. Example: WithoutModelHooks(Orders.Query()).UpdateAll(ctx,db,Orders.TotalCents.Set(0)).
func WithoutModelHooks[M any](q Query[M]) Query[M] { q.state.ast.SkipModelHooks = true; return q }

// ForceDelete physically deletes one key within the query's current visibility.
// Example: ForceDelete(ctx,db,Orders.Query().WithTrashed(),Orders.Key(),id).
func ForceDelete[M any, K comparable](ctx context.Context, writer database.Transactor, q Query[M], key Key[M, K], id K) (M, error) {
	if q.state.table == nil || key.key.core == nil || key.key.core != q.state.table.primary {
		return *new(M), invalid("force delete requires this table's primary key")
	}
	return writeOne[M](ctx, writer, q.Where(key.Eq(id)).state, querycore.ForceDeleteModel, nil)
}
