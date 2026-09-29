package factory

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// MaxHooks bounds a factory's after-create hooks.
const MaxHooks = 16

// AfterCreate runs after a model was inserted, inside the transaction that
// inserted it; writer is that transaction. It can create related records or
// return a reloaded model. A returned error or panic rolls back the insert, the
// hooks' own writes and, for CreateMany, the whole batch.
type AfterCreate[M any] func(context.Context, database.Transactor, M) (M, error)

// AfterCreating returns a derived factory that runs hooks, in order, after each
// Create and after every model of CreateMany. The factory is unchanged.
func (f *Factory[M, D]) AfterCreating(hooks ...AfterCreate[M]) (*Factory[M, D], error) {
	if err := f.valid(); err != nil {
		return nil, err
	}
	if len(hooks) > MaxHooks-len(f.after) {
		return nil, fault.New(fault.Invalid, "factory hook count exceeds its bound")
	}
	for _, hook := range hooks {
		if hook == nil {
			return nil, fault.New(fault.Invalid, "nil factory hook")
		}
	}
	result := *f
	result.after = append(slices.Clone(f.after), hooks...)
	return &result, nil
}

func (f *Factory[M, D]) afterCreate(ctx context.Context, writer database.Transactor, model M) (M, error) {
	for _, hook := range f.after {
		if err := ctx.Err(); err != nil {
			return *new(M), err
		}
		err := callback.Isolated("factory after-create hook", func() error {
			var err error
			model, err = hook(ctx, writer, model)
			return err
		})
		if err != nil {
			return *new(M), err
		}
	}
	return model, nil
}

// DraftWith builds one draft with per-call states applied after the factory's
// own states. The overrides are not retained.
func (f *Factory[M, D]) DraftWith(ctx context.Context, overrides ...State[D]) (D, error) {
	derived, err := f.WithStates(overrides...)
	if err != nil {
		return *new(D), err
	}
	return derived.Draft(ctx)
}

// CreateWith creates one model with per-call attribute overrides, for example
// `func(_ context.Context, d UserDraft) (UserDraft, error) { return d.SetName("Ada"), nil }`.
// The overrides are not retained by the factory.
func (f *Factory[M, D]) CreateWith(ctx context.Context, writer database.Transactor, overrides ...State[D]) (M, error) {
	derived, err := f.WithStates(overrides...)
	if err != nil {
		return *new(M), err
	}
	return derived.Create(ctx, writer)
}

// CreateFor creates one parent through its own factory, then one model that
// belongs to it. assign copies the parent's key into the child's draft, for
// example `func(d PostDraft, u User) PostDraft { return d.SetAuthorID(u.ID) }`.
// Both inserts use writer and their ordinary model lifecycle.
func CreateFor[M Model[M], D query.CreateDraft[M], P Model[P], PD query.CreateDraft[P]](ctx context.Context, writer database.Transactor, child *Factory[M, D], parent *Factory[P, PD], assign func(D, P) D) (M, P, error) {
	if assign == nil {
		return *new(M), *new(P), fault.New(fault.Invalid, "factory relationship requires an assignment")
	}
	owner, err := parent.Create(ctx, writer)
	if err != nil {
		return *new(M), *new(P), err
	}
	created, err := child.CreateWith(ctx, writer, belongsTo(owner, assign))
	if err != nil {
		return *new(M), *new(P), err
	}
	return created, owner, nil
}

// CreateHas creates one model through f, then count children that belong to it
// through the child factory in one bounded CreateMany batch.
func CreateHas[M Model[M], D query.CreateDraft[M], C Model[C], CD query.CreateDraft[C]](ctx context.Context, writer database.Transactor, f *Factory[M, D], child *Factory[C, CD], count int, assign func(CD, M) CD) (M, []C, error) {
	if assign == nil {
		return *new(M), nil, fault.New(fault.Invalid, "factory relationship requires an assignment")
	}
	owner, err := f.Create(ctx, writer)
	if err != nil {
		return *new(M), nil, err
	}
	children, err := child.WithStates(belongsTo(owner, assign))
	if err != nil {
		return *new(M), nil, err
	}
	created, err := children.CreateMany(ctx, writer, count)
	if err != nil {
		return *new(M), nil, err
	}
	return owner, created, nil
}

func belongsTo[D, P any](owner P, assign func(D, P) D) State[D] {
	return func(_ context.Context, draft D) (D, error) { return assign(draft, owner), nil }
}
