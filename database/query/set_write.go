package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Adjustment is a typed delta for one stored numeric column in a set-based
// Increment or Decrement: column = column + delta (or - delta). SQL NULL
// stays NULL. Build it from a generated field, for example Views.By(1).
type Adjustment[M any] struct {
	field fieldRef
	bind  func() (driver.Value, error)
}

// By prepares an exact-number adjustment of this field.
func (f ExactField[M, V]) By(delta V) Adjustment[M] {
	return Adjustment[M]{field: f.ref, bind: func() (driver.Value, error) { return f.codec.Bind(delta) }}
}

// By prepares a nullable exact-number adjustment; NULL stays NULL.
func (f NullableExactField[M, V]) By(delta V) Adjustment[M] {
	return Adjustment[M]{field: f.ref, bind: func() (driver.Value, error) { return f.codec.Bind(delta) }}
}

// By prepares a floating-point adjustment of this field.
func (f FloatField[M, V]) By(delta V) Adjustment[M] {
	return Adjustment[M]{field: f.ref, bind: func() (driver.Value, error) { return f.codec.Bind(delta) }}
}

type adjustment struct {
	field    fieldRef
	bind     func() (driver.Value, error)
	subtract bool
}

// adjustSet compiles "column" = "column" + $n for a declared, non-key column
// that the same statement does not also assign.
func (p mutationPlan[M]) adjustSet(c *compiler, assigned map[string]Assignment[M]) (string, error) {
	a := p.adjust
	if err := a.field.validate(p.query.table); err != nil {
		return "", err
	}
	column, declared := c.columns[a.field.column]
	if _, repeated := assigned[a.field.column]; !declared || repeated || a.field.column == p.query.definition.primary || a.bind == nil {
		return "", fault.New(fault.Invalid, "numeric adjustment requires a declared, unassigned non-key field")
	}
	bound, err := a.bind()
	if err != nil {
		return "", err
	}
	if bound == nil {
		return "", fault.New(fault.Invalid, "numeric adjustment requires a non-NULL delta")
	}
	parameter, err := c.parameter(bound)
	if err != nil {
		return "", err
	}
	op := " + "
	if a.subtract {
		op = " - "
	}
	return quoted(column.Name) + " = " + quoted(column.Name) + op + parameter, nil
}

// validate applies the shared mutation rules; a set-based adjustment may be
// the only change of an update.
func (p mutationPlan[M]) validate(c *compiler) (map[string]Assignment[M], error) {
	if p.adjust != nil && p.kind.sqlKind() == updateModel {
		return p.query.validateMutationShape(p.kind, p.mutation, c)
	}
	return p.query.validateMutation(p.kind, p.mutation, c)
}

// WithoutModelHooks acknowledges that set-based writes (UpdateAll, DeleteAll,
// ForceDeleteAll, Increment, Decrement and their runtime forms) run one SQL
// statement without per-model hooks, provider observers or change capture on
// a model that declares them. It has no other effect; per-model writes on such
// a query are rejected so the option cannot be mistaken for disabling hooks.
func (q Query[M]) WithoutModelHooks() Query[M] { q.skipModelHooks = true; return q }

// PatchAll assigns the mutation to every row matching the query's filters,
// global scopes and soft-delete visibility in one UPDATE, applying managed
// update timestamps and field mutators once. It returns the affected count.
func (q Query[M]) PatchAll(ctx context.Context, writer database.Transactor, mutation Mutation[M]) (int64, error) {
	return q.setWrite(ctx, writer, updateModel, mutation, nil)
}

// AdjustAll adds (or with subtract, removes) a typed delta to one numeric
// column of every matching row, with optional extra assignments.
func (q Query[M]) AdjustAll(ctx context.Context, writer database.Transactor, adjust Adjustment[M], subtract bool, mutation Mutation[M]) (int64, error) {
	if adjust.bind == nil {
		return 0, fault.New(fault.Invalid, "numeric adjustment requires a generated field")
	}
	return q.setWrite(ctx, writer, updateModel, mutation, &adjustment{field: adjust.field, bind: adjust.bind, subtract: subtract})
}

// RemoveAll soft-deletes every matching active row of a soft-delete model, or
// physically deletes every matching row of an ordinary model, in one statement.
func (q Query[M]) RemoveAll(ctx context.Context, writer database.Transactor) (int64, error) {
	selected, kind := q.removal()
	return selected.setWrite(ctx, writer, kind, Mutation[M]{}, nil)
}

// ForceRemoveAll physically deletes every row matching the current visibility
// of a soft-delete model. Use WithTrashed to include deleted rows.
func (q Query[M]) ForceRemoveAll(ctx context.Context, writer database.Transactor) (int64, error) {
	return q.setWrite(ctx, writer, forceDeleteModel, Mutation[M]{}, nil)
}

func (q Query[M]) setWrite(ctx context.Context, writer database.Transactor, kind mutationKind, mutation Mutation[M], adjust *adjustment) (int64, error) {
	if err := writeContext(ctx, writer); err != nil {
		return 0, err
	}
	q = q.inContext(ctx)
	if q.definition == nil {
		return 0, fault.New(fault.Invalid, "set-based writes require model metadata")
	}
	if err := q.setWriteLifecycle(writer, kind); err != nil {
		return 0, err
	}
	if _, err := q.modelWriteCompiler(kind, false); err != nil {
		return 0, err
	}
	plan := mutationPlan[M]{query: q, kind: kind, mutation: mutation, setBased: true, countOnly: true, adjust: adjust}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (int64, error) {
		statement, err := prepareMutation(ctx, &plan, transactionClock(tx))
		if err != nil {
			return 0, err
		}
		result, err := tx.Exec(ctx, statement.sql, statement.arguments...)
		return result.RowsAffected, err
	})
}

// setWriteLifecycle rejects a set-based write that would silently skip
// declared hooks or registered observers unless WithoutModelHooks acknowledges
// it. An executor that cannot prove observers absent counts as observed.
func (q Query[M]) setWriteLifecycle(writer database.Transactor, kind mutationKind) error {
	if q.skipModelHooks {
		return nil
	}
	observers, known := writerObservers(writer)
	if q.definition.hasWriteHooks || !known || observedBy[M](observers, kind) {
		return fault.New(fault.Invalid, "set-based writes skip per-model hooks and observers; use the Each methods or acknowledge with WithoutModelHooks")
	}
	return nil
}
