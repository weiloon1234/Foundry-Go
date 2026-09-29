package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// FirstOrInsert returns the first primary-ordered matching model, or creates
// one through its ordinary lifecycle. Generated FirstOrCreate methods retain
// concrete model drafts. Write-owned lookup skips Retrieved callbacks. Omitted
// draft fields default from the query's top-level equality filters and active
// global scopes; the stored model must match the query. Concurrent absence is not locked:
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
	q = q.inContext(ctx)
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
		return executeMutation(ctx, enclosingTransaction{tx}, mutationPlan[M]{query: q.Where(primary), kind: updateModel, mutation: mutation})
	})
}

// InsertOrFirst creates a model through its ordinary lifecycle, and when a
// unique constraint rejects the creation it returns the first primary-ordered
// model matching the query instead. Generated CreateOrFirst methods retain the
// concrete draft. Unlike FirstOrInsert, it inserts first, so a concurrent
// creation of the same unique key is resolved rather than reported: the insert
// runs in one savepoint, a unique violation rolls back only that savepoint, and
// the committed competitor is then read. Under READ COMMITTED the competitor is
// visible; under REPEATABLE READ or SERIALIZABLE a competitor committed after
// the snapshot is not, and the unique violation is returned. The query must
// select the row that holds the conflicting unique key; if no row matches, the
// violation is returned. A created model must satisfy the query. Write-owned
// lookup skips Retrieved callbacks.
//
// Only the model's own INSERT statement failing with a unique violation of an
// index on the model's table is resolved. Failures from hooks and observers
// (including their own writes) and violations raised elsewhere, such as by a
// trigger writing another table, are returned unchanged.
func (q Query[M]) InsertOrFirst(ctx context.Context, writer database.Transactor, create CreateDraft[M]) (M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(M), err
	}
	q = q.inContext(ctx)
	if nilDescriptor(create) {
		return *new(M), fault.New(fault.Invalid, "lookup creation requires a typed draft")
	}
	if _, err := q.modelWriteCompiler(updateModel, false); err != nil {
		return *new(M), err
	}
	statement, err := q.firstQuery().Compile()
	if err != nil {
		return *new(M), err
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (M, error) {
		defaults, err := q.lookupDefaults(ctx)
		if err != nil {
			return *new(M), err
		}
		mutation, err := create.FoundryCreateMutation(defaults)
		if err != nil {
			return *new(M), err
		}
		// tx (not the enclosing transaction) gives the insert its own savepoint,
		// so a unique violation leaves the lookup transaction usable.
		var statementErr error
		created, insertErr := executeMutation(ctx, tx, mutationPlan[M]{query: ForModel(*q.definition), kind: insertModel, mutation: mutation, statementFailure: &statementErr})
		if insertErr == nil {
			return q.requireLookupMatch(ctx, tx, created)
		}
		if statementErr == nil {
			return *new(M), insertErr
		}
		// A failed savepoint rollback poisons tx, so the reads below then fail too.
		conflict, err := q.ownUniqueViolation(ctx, tx, statementErr)
		if err != nil {
			return *new(M), errors.Join(insertErr, err)
		}
		if !conflict {
			return *new(M), insertErr
		}
		models, err := returningModels(ctx, tx, statement, q.definition.scan, 0, 1)
		if err != nil {
			return *new(M), errors.Join(insertErr, err)
		}
		if len(models) == 0 {
			return *new(M), insertErr
		}
		return models[0], nil
	})
}

// ownUniqueViolation classifies the model INSERT statement's own failure: it
// must carry a unique violation whose constraint is a unique index of the
// model's table. Error inspection is bounded and contains extension methods.
func (q Query[M]) ownUniqueViolation(ctx context.Context, tx *database.Tx, statementErr error) (bool, error) {
	var constraint string
	if err := callback.Isolated("classify lookup insert failure", func() error {
		failure, found, complete := errorgraph.As[*database.Error](statementErr)
		if complete && found && failure != nil && failure.Code() == database.UniqueViolation {
			constraint = failure.Constraint()
		}
		return nil
	}); err != nil || constraint == "" {
		return false, err
	}
	var owned bool
	err := database.ScanOne(ctx, tx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class c ON c.oid = i.indexrelid WHERE i.indisunique AND i.indrelid = to_regclass($1) AND c.relname = $2)`, []any{quotedTable(q.table), constraint}, &owned)
	return owned, err
}

// requireLookupMatch verifies a created model satisfies the lookup query.
func (q Query[M]) requireLookupMatch(ctx context.Context, tx *database.Tx, created M) (M, error) {
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

func (q Query[M]) createLookupModel(ctx context.Context, tx *database.Tx, draft CreateDraft[M]) (M, error) {
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	defaults, err := q.lookupDefaults(ctx)
	if err != nil {
		return *new(M), err
	}
	mutation, err := draft.FoundryCreateMutation(defaults)
	if err != nil {
		return *new(M), err
	}
	created, err := ForModel(*q.definition).Insert(ctx, enclosingTransaction{tx}, mutation)
	if err != nil {
		return *new(M), err
	}
	return q.requireLookupMatch(ctx, tx, created)
}

// lookupDefaults turns the lookup's top-level equality filters, including
// active global scopes resolved with ctx, into creation defaults, as
// Laravel merges firstOrCreate attributes. Explicit draft inputs still win.
// Fields with custom mutators are skipped: a stored value is not a mutator
// input. Conflicting equalities on one field supply no default.
func (q Query[M]) lookupDefaults(ctx context.Context) (Mutation[M], error) {
	values := make(map[string]driver.Value)
	conflicted := make(map[string]bool)
	var order []string
	var collect func(expression) error
	collect = func(e expression) error {
		switch e := e.(type) {
		case junction:
			if e.any {
				return nil
			}
			for _, child := range e.children {
				if err := collect(child); err != nil {
					return err
				}
			}
		case scopeNode:
			resolved, err := e.resolved(ctx)
			if err != nil {
				return err
			}
			return collect(resolved)
		case comparison:
			field, ok := e.operand.(fieldRef)
			if !ok || field.table != q.table || e.operator != equal || len(e.values) != 1 || e.bind == nil {
				return nil
			}
			declared, found := q.definition.modelField(field.column)
			if !found || declared.assignment == nil || declared.mutator.apply != nil {
				return nil
			}
			raw, err := e.bind(e.values[0])
			if err != nil || raw == nil {
				return err
			}
			if previous, seen := values[field.column]; seen {
				if !driverValuesEqual(previous, raw) {
					conflicted[field.column] = true
				}
				return nil
			}
			values[field.column] = raw
			order = append(order, field.column)
		}
		return nil
	}
	for _, e := range append(slices.Clone(q.predicates), q.scopePredicates()...) {
		if err := collect(e); err != nil {
			return Mutation[M]{}, err
		}
	}
	var defaults Mutation[M]
	for _, column := range order {
		if conflicted[column] {
			continue
		}
		var err error
		if defaults, err = q.withModelValue(defaults, column, values[column]); err != nil {
			return Mutation[M]{}, err
		}
	}
	return defaults, nil
}

func driverValuesEqual(a, b driver.Value) bool {
	left, leftErr := encodeModelKey(a)
	right, rightErr := encodeModelKey(b)
	return leftErr == nil && rightErr == nil && left == right
}
