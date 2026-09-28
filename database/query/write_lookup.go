package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// FirstOrInsert returns the first primary-ordered matching model, or creates
// one through its ordinary lifecycle. Generated FirstOrCreate methods retain
// concrete model drafts. Write-owned lookup skips Retrieved callbacks. The
// explicit creation draft must produce a stored model matching the query; no
// field inputs are guessed from predicates. Concurrent absence is not locked:
// physical unique constraints and ordinary conflict errors remain authoritative.
func (q Query[M]) FirstOrInsert(ctx context.Context, writer database.Transactor, create CreateDraft[M]) (M, error) {
	return q.writeLookup(ctx, writer, create, nil)
}

// PatchOrInsert updates the first primary-ordered matching model through a
// typed callback, or creates one from the separate creation draft. Generated
// UpdateOrCreate methods expose the concrete callback draft. Only the selected
// branch prepares its draft. This is a lifecycle-aware transaction, not a
// single-statement upsert; there is no hidden retry after a competing insert.
func (q Query[M]) PatchOrInsert(ctx context.Context, writer database.Transactor, create CreateDraft[M], update func(context.Context, *database.Tx, M) (Mutation[M], error)) (M, error) {
	if update == nil {
		return *new(M), fault.New(fault.Invalid, "lookup update requires a draft callback")
	}
	return q.writeLookup(ctx, writer, create, update)
}

func (q Query[M]) writeLookup(ctx context.Context, writer database.Transactor, create CreateDraft[M], update func(context.Context, *database.Tx, M) (Mutation[M], error)) (M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(M), err
	}
	if nilDescriptor(create) {
		return *new(M), fault.New(fault.Invalid, "lookup creation requires a typed draft")
	}
	if _, err := q.modelWriteCompiler(updateModel, false); err != nil {
		return *new(M), err
	}
	statement, err := q.firstQuery().ForUpdate().Compile()
	if err != nil {
		return *new(M), err
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (M, error) {
		models, err := returningModels(ctx, tx, statement, q.definition.scan, 0, 1)
		if err != nil {
			return *new(M), err
		}
		if len(models) == 0 {
			return q.createLookupModel(ctx, tx, create)
		}
		current := models[0]
		if update == nil {
			return current, nil
		}
		primary, err := modelWritePredicate(q, current)
		if err != nil {
			return *new(M), err
		}
		mutation, err := modelUpdateMutation(ctx, tx, current, update)
		if err != nil {
			return *new(M), err
		}
		return executeMutation(ctx, tx, mutationPlan[M]{query: q.Where(primary), kind: updateModel, mutation: mutation})
	})
}

func (q Query[M]) createLookupModel(ctx context.Context, tx *database.Tx, draft CreateDraft[M]) (M, error) {
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	mutation, err := draft.FoundryCreateMutation(Mutation[M]{})
	if err != nil {
		return *new(M), err
	}
	created, err := ForModel(*q.definition).Insert(ctx, tx, mutation)
	if err != nil {
		return *new(M), err
	}
	primary, err := modelWritePredicate(q, created)
	if err != nil {
		return *new(M), err
	}
	matches, err := q.Where(primary).Exists(ctx, tx)
	if err != nil {
		return *new(M), err
	}
	if !matches {
		return *new(M), fault.New(fault.Invalid, "created model does not satisfy lookup predicates and visibility")
	}
	return created, nil
}
