package orm

import (
	"context"
	"errors"
	"slices"
	"time"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Insert creates and hydrates one model. Example: Orders.Insert(ctx,tx,Orders.TotalCents.Set(10),Orders.BuyerID.Set(buyer)).
func (t Table[M, K]) Insert(ctx context.Context, tx database.Transactor, as ...Assignment[M]) (M, error) {
	return writeOne[M](ctx, tx, newQuery(t.core), querycore.InsertModel, as)
}

// Update patches one typed key and hydrates it. Example: Orders.Update(ctx,tx,id,Orders.TotalCents.Set(20)).
func (t Table[M, K]) Update(ctx context.Context, tx database.Transactor, key K, as ...Assignment[M]) (M, error) {
	return writeOne[M](ctx, tx, t.Query().Where(t.Key().Eq(key)).state, querycore.UpdateModel, as)
}

// Delete removes one key, using soft deletion when declared. Example: Orders.Delete(ctx,tx,id).
func (t Table[M, K]) Delete(ctx context.Context, tx database.Transactor, key K) (M, error) {
	return writeOne[M](ctx, tx, t.Query().Where(t.Key().Eq(key)).state, querycore.DeleteModel, nil)
}

// UpdateAll patches every matching row. Example: Orders.Query().Where(Orders.TotalCents.Eq(0)).UpdateAll(ctx,tx,Orders.TotalCents.Set(1)).
func (q Query[M]) UpdateAll(ctx context.Context, tx database.Transactor, as ...Assignment[M]) (int64, error) {
	return writeAll(ctx, tx, q.state, querycore.UpdateModel, as)
}

// DeleteAll removes matching rows, respecting soft deletion. Example: Orders.Query().Where(Orders.ID.Eq(id)).DeleteAll(ctx,tx).
func (q Query[M]) DeleteAll(ctx context.Context, tx database.Transactor) (int64, error) {
	return writeAll[M](ctx, tx, q.state, querycore.DeleteModel, nil)
}

// WriteError preserves a candidate when persistence needs reconciliation. Example: errors.As(err,&writeError).
type WriteError[R any] struct {
	cause     error
	candidate R
}

// Error omits model values. Example: writeError.Error().
func (e *WriteError[R]) Error() string {
	return "model write requires transaction outcome reconciliation"
}

// GoString omits model values. Example: fmt.Sprintf("%#v",writeError).
func (e *WriteError[R]) GoString() string { return e.Error() }

// Unwrap retains the original transaction outcome. Example: errors.As(writeError, &databaseError).
func (e *WriteError[R]) Unwrap() error { return e.cause }

// Candidate is reconciliation data, not proof of persistence. Example: writeError.Candidate().
func (e *WriteError[R]) Candidate() R { return e.candidate }

type writePlan struct {
	query       queryState
	kind        querycore.MutationKind
	assignments []querycore.MutationAssignment
	many        bool
	adjust      *querycore.Adjustment
	bounded     bool
	batch       [][]querycore.MutationAssignment
}

func planWrite[M any](q queryState, kind querycore.MutationKind, as []Assignment[M], many bool) (writePlan, error) {
	if err := validateQuery(q); err != nil {
		return writePlan{}, err
	}
	if !many && q.ast.SkipModelHooks {
		return writePlan{}, invalid("WithoutModelHooks is only an acknowledgement for set writes")
	}
	p := writePlan{query: q, kind: kind, many: many, assignments: make([]querycore.MutationAssignment, 0, len(as))}
	for _, a := range as {
		if a.err != nil {
			return writePlan{}, a.err
		}
		if a.owner != q.table {
			return writePlan{}, invalid("assignment belongs to a different table declaration")
		}
		if a.adjust != nil {
			if kind != querycore.UpdateModel || p.adjust != nil {
				return writePlan{}, invalid("update accepts one numeric adjustment")
			}
			for _, field := range q.table.mutators {
				if field.Column == a.adjust.Field.Column {
					return writePlan{}, invalid("numeric adjustment cannot bypass a Go mutator")
				}
			}
			p.adjust = a.adjust
			if a.guard != nil {
				p.bounded = true
				p.query.ast.Predicates = append(slices.Clone(p.query.ast.Predicates), a.guard)
			}
		} else {
			p.assignments = append(p.assignments, a.core)
		}
	}
	if many && querycore.HasEncryptedAssignments(p.assignments) {
		return writePlan{}, querycore.SetEncryptedError()
	}
	if kind == querycore.DeleteModel && q.ast.HasSoftDeletes() {
		p.kind = querycore.SoftDeleteModel
		if p.query.ast.SoftDeleteScope == querycore.TrashedRecords {
			p.query.ast.Predicates = append(slices.Clone(q.ast.Predicates), q.ast.DeletionPredicate(querycore.IsNull))
		} else {
			p.query.ast.SoftDeleteScope = querycore.ActiveRecords
		}
	}
	if kind == querycore.RestoreModel {
		p.query.ast.SoftDeleteScope = querycore.TrashedRecords
	}
	if (kind == querycore.RestoreModel || kind == querycore.ForceDeleteModel) && !q.ast.HasSoftDeletes() {
		return writePlan{}, invalid("operation requires soft deletes")
	}
	c, err := p.query.ast.WriteCompiler(p.kind.SqlKind(), !many, len(p.query.relations) > 0)
	if err != nil {
		return writePlan{}, err
	}
	assigned, err := p.query.ast.ValidateMutationShape(p.kind, p.assignments, &c)
	if err != nil {
		return writePlan{}, err
	}
	if p.adjust != nil {
		if err := p.query.ast.ValidateAdjustment(p.adjust, &c, assigned); err != nil {
			return writePlan{}, err
		}
	}
	return p, nil
}
func (p writePlan) needsClock() bool {
	return p.kind.SqlKind() != querycore.DeleteModel && (p.query.table.model.Timestamps != nil || p.kind == querycore.SoftDeleteModel)
}
func (p writePlan) conventions(ctx context.Context, source clock.Clock) (writePlan, error) {
	if p.batch != nil {
		if p.needsClock() {
			if source == nil {
				return p, invalid("managed timestamps require the owning application clock")
			}
			source = fixedNow{source.Now()}
		}
		batch := make([][]querycore.MutationAssignment, len(p.batch))
		for i, row := range p.batch {
			item := p
			item.batch = nil
			item.assignments = row
			current, err := item.conventions(ctx, source)
			if err != nil {
				return p, err
			}
			batch[i] = current.assignments
		}
		p.batch = batch
		return p, nil
	}
	p.assignments = slices.Clone(p.assignments)
	if p.kind == querycore.InsertModel {
		for _, c := range p.query.table.columns {
			if c.metadata.Name == p.query.table.model.Primary && !c.metadata.DatabaseDefault && c.uuid && (c.mutator.Input == nil || c.mutator.Input == c.field.Type) && !p.present(c.metadata.Name) {
				id, err := model.NewID[struct{}]()
				if err != nil {
					return p, err
				}
				if err = p.setAutomatic(c.metadata.Name, id.String()); err != nil {
					return p, err
				}
			}
		}
	}
	if p.kind == querycore.RestoreModel {
		if err := p.setAutomatic(p.query.table.model.SoftDelete.Name, nil); err != nil {
			return p, err
		}
	}
	if !p.needsClock() {
		return p, nil
	}
	if source == nil || ctx == nil {
		return p, invalid("managed timestamps require the owning application clock")
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	now := source.Now().UTC().Truncate(time.Microsecond)
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if _, err := codec.Time().Bind(now); err != nil {
		return p, err
	}
	if p.kind == querycore.SoftDeleteModel {
		if err := p.setAutomatic(p.query.table.model.SoftDelete.Name, now); err != nil {
			return p, err
		}
	}
	if ts := p.query.table.model.Timestamps; ts != nil {
		if p.kind == querycore.InsertModel && !p.present(ts.Created) {
			if err := p.setAutomatic(ts.Created, now); err != nil {
				return p, err
			}
		}
		if err := p.setAutomatic(ts.Updated, now); err != nil {
			return p, err
		}
	}
	return p, nil
}
func (p writePlan) present(name string) bool {
	return slices.ContainsFunc(p.assignments, func(a querycore.MutationAssignment) bool { return a.Field.Column == name })
}
func (p *writePlan) setAutomatic(name string, raw any) error {
	for _, c := range p.query.table.columns {
		if c.metadata.Name == name {
			v, err := c.automatic(raw)
			if err != nil {
				return err
			}
			a := querycore.MutationAssignment{Field: querycore.FieldRef{Table: p.query.ast.Table, Column: name}, AssignmentValue: v}
			for i, old := range p.assignments {
				if old.Field.Column == name {
					p.assignments[i] = a
					return nil
				}
			}
			p.assignments = append(p.assignments, a)
			return nil
		}
	}
	return invalid("automatic assignment has no declared codec")
}

// validateAssignments is shared by effective-draft preflight and final compilation.
// It checks shape/required fields without binding, mutating or sealing values.
func (p writePlan) validateAssignments(c *querycore.Compiler) (map[string]querycore.MutationAssignment, error) {
	if p.adjust == nil {
		return p.query.ast.ValidateMutation(p.kind, p.assignments, c)
	}
	assigned, err := p.query.ast.ValidateMutationShape(p.kind, p.assignments, c)
	if err != nil {
		return nil, err
	}
	if err = p.query.ast.ValidateAdjustment(p.adjust, c, assigned); err != nil {
		return nil, err
	}
	return assigned, nil
}
func (p writePlan) validateEffective() error {
	c, err := p.query.ast.WriteCompiler(p.kind.SqlKind(), !p.many, len(p.query.relations) > 0)
	if err != nil {
		return err
	}
	if p.batch != nil {
		for _, row := range p.batch {
			if _, err = p.query.ast.ValidateMutation(querycore.InsertModel, row, &c); err != nil {
				return err
			}
		}
		return nil
	}
	_, err = p.validateAssignments(&c)
	return err
}

func (p writePlan) compile() (querycore.Statement, error) {
	c, err := p.query.ast.WriteCompiler(p.kind.SqlKind(), !p.many, len(p.query.relations) > 0)
	if err != nil {
		return querycore.Statement{}, err
	}
	if p.batch != nil {
		return p.compileBatch(c)
	}

	assigned, err := p.validateAssignments(&c)
	if err != nil {
		return querycore.Statement{}, err
	}

	node := p.query.ast.ModelSelect(p.query.table.selections)
	if p.kind == querycore.InsertModel {
		union := make(map[string]bool, len(assigned))
		for name := range assigned {
			union[name] = true
		}
		return p.query.ast.CompileInsert(c, []map[string]querycore.MutationAssignment{assigned}, union, node, nil)
	}
	return p.query.ast.CompileMutation(c, node, p.kind, p.assignments, assigned, p.adjust, p.many)
}
func writeOne[M any](ctx context.Context, writer database.Transactor, q queryState, kind querycore.MutationKind, as []Assignment[M]) (M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(M), err
	}
	p, err := planWrite(q, kind, as, false)
	if err != nil {
		return *new(M), err
	}
	var candidate M
	present := false
	err, reconcile := executeWrite(ctx, writer, p, func(row database.Row) error {
		var item M
		if err := hydrate(q.table, row, unsafe.Pointer(&item)); err != nil {
			return err
		}
		candidate = item
		present = true
		return nil
	}, nil, modelCallbacks(q.table, unsafe.Pointer(&candidate)))
	if err != nil {
		if present && reconcile {
			return *new(M), &WriteError[M]{cause: err, candidate: candidate}
		}
		return *new(M), err
	}
	return candidate, nil
}
func writeAll[M any](ctx context.Context, writer database.Transactor, q queryState, kind querycore.MutationKind, as []Assignment[M]) (int64, error) {
	if err := writeContext(ctx, writer); err != nil {
		return 0, err
	}
	p, err := planWrite(q, kind, as, true)
	if err != nil {
		return 0, err
	}
	set, known := ownerObservers(writer)
	if !q.ast.SkipModelHooks && (q.table.hooks != nil || !known || observed[M](set, p.kind)) {
		return 0, invalid("set-based writes skip per-model hooks and observers; acknowledge with WithoutModelHooks")
	}
	var count int64
	err, reconcile := executeWrite(ctx, writer, p, nil, &count, nil)
	if err != nil {
		if reconcile {
			return 0, &WriteError[int64]{cause: err, candidate: count}
		}
		return 0, err
	}
	return count, nil
}
func executeWrite(ctx context.Context, writer database.Transactor, p writePlan, scan func(database.Row) error, count *int64, callbacks *writeCallbacks) (error, bool) {
	if ctx == nil || writer == nil {
		return invalid("write requires a context and transactor"), false
	}
	if err := ctx.Err(); err != nil {
		return err, false
	}
	p.query.ast.ScopeContext = ctx
	set, known := ownerObservers(writer)
	hooked := callbacks != nil && (!known || callbacks.needed(set, p.kind))
	// Exact pool owners can confirm a single INSERT without an explicit transaction.
	if p.kind == querycore.InsertModel && !p.many && !p.encrypted() && !p.mutated() && !hooked {
		switch owner := writer.(type) {
		case *database.DB:
			if owner == nil {
				return invalid("nil transactor"), false
			}
			return autocommit(ctx, p, owner.Clock(), owner.FoundryAutocommitQuery, scan)
		case database.PrimaryExecutor:
			return autocommit(ctx, p, owner.Clock(), owner.FoundryAutocommitQuery, scan)
		}
	}
	var prepared querycore.Statement
	var err error
	if !p.needsClock() && !p.encrypted() && !p.mutated() && !hooked {
		p, err = p.conventions(ctx, nil)
		if err == nil {
			prepared, err = p.compile()
		}
		if err != nil {
			return err, false
		}
	}
	completed := false
	err = writer.Transaction(ctx, func(tx *database.Tx) error {
		if tx == nil {
			return invalid("transactor supplied no transaction")
		}
		if hooked {
			if err := callbacks.before(ctx, tx, &p); err != nil {
				return err
			}
			ctx = p.query.ast.ScopeContext
		}
		s := prepared
		if p.needsClock() || p.encrypted() || p.mutated() || hooked {
			current, e := p.conventions(ctx, tx.Clock())
			if e != nil {
				return e
			}
			if e = current.validateEffective(); e != nil {
				return e
			}
			current, e = current.mutate(ctx)
			if e != nil {
				return e
			}
			current, e = current.seal(ctx, tx)
			if e != nil {
				return e
			}
			p = current
			s, e = current.compile()
			if e != nil {
				return e
			}
		}
		if p.many {
			result, e := tx.Exec(s.Context(ctx), s.SQLText, s.Bindings...)
			if e != nil {
				return e
			}
			*count = result.RowsAffected
		} else {
			rows, e := tx.Query(s.Context(ctx), s.SQLText, s.Bindings...)
			if e != nil {
				return e
			}
			if e = returningN(ctx, rows, scan, p.returnCount(), p.bounded); e != nil {

				return e
			}
		}
		if hooked {
			if err := callbacks.after(ctx, tx, p); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil && completed {
		return inspectOutcome(err, false)
	}
	if err == nil && !completed {
		return invalid("transactor returned without a successful write"), false
	}
	return err, false
}
func returning(ctx context.Context, rows rowSource, scan func(database.Row) error) error {
	return returningN(ctx, rows, scan, 1, false)
}
func returningN(ctx context.Context, rows rowSource, scan func(database.Row) error, expected int, bounded bool) error {
	n := 0
	err := consume(ctx, rows, func(row database.Row) error {
		if n >= expected {
			return database.NewError("model write returned too many rows", database.TooManyRows)
		}
		n++
		return scan(row)
	})
	if err == nil && n < expected {
		if n == 0 && bounded {
			return boundExceeded()
		}
		return database.NewError("model write returned too few rows", database.NotFound)
	}
	return err
}
func (p writePlan) returnCount() int {
	if p.batch != nil {
		return len(p.batch)
	}
	return 1
}

type fixedNow struct{ instant time.Time }

func (c fixedNow) Now() time.Time { return c.instant }

func (p writePlan) compileBatch(c querycore.Compiler) (querycore.Statement, error) {
	rows := make([]map[string]querycore.MutationAssignment, len(p.batch))
	union := map[string]bool{}
	for i, row := range p.batch {
		assigned, err := p.query.ast.ValidateMutation(querycore.InsertModel, row, &c)
		if err != nil {
			return querycore.Statement{}, err
		}
		rows[i] = assigned
		for name := range assigned {
			union[name] = true
		}
	}
	if len(union) == 0 && len(rows) > 1 {
		union[p.query.table.model.Primary] = true
	}
	if len(rows) > 0 && len(union) > querycore.MaxParameters/len(rows) {
		return querycore.Statement{}, invalid("model insert exceeds its cell bound")
	}
	return p.query.ast.CompileInsert(c, rows, union, p.query.ast.ModelSelect(p.query.table.selections), nil)
}

// InsertMany inserts one bounded atomic batch and hydrates RETURNING rows.
func (t Table[M, K]) InsertMany(ctx context.Context, tx database.Transactor, rows [][]Assignment[M]) ([]M, error) {
	return insertMany[M](ctx, tx, newQuery(t.core), rows)
}
func insertMany[M any](ctx context.Context, writer database.Transactor, q queryState, rows [][]Assignment[M]) ([]M, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	if len(rows) > query.MaxInsertRows {
		return nil, invalid("model insert exceeds its row bound")
	}
	p := writePlan{query: q, kind: querycore.InsertModel, batch: make([][]querycore.MutationAssignment, len(rows))}
	cells := 0
	for i, row := range rows {
		cells += len(row)
		if cells > querycore.MaxParameters {
			return nil, invalid("model insert exceeds its cell bound")
		}
		item, err := planWrite(q, querycore.InsertModel, row, false)
		if err != nil {
			return nil, err
		}
		p.batch[i] = item.assignments
	}
	if len(rows) == 0 {
		_, err := p.compile()
		return []M{}, err
	}
	items := make([]M, 0, len(rows))
	err, reconcile := executeWrite(ctx, writer, p, func(row database.Row) error {
		var item M
		if err := hydrate(q.table, row, unsafe.Pointer(&item)); err != nil {
			return err
		}
		items = append(items, item)
		return nil
	}, nil, nil)
	if err != nil {
		if reconcile && len(items) > 0 {
			return nil, &WriteError[[]M]{cause: err, candidate: items}
		}
		return nil, err
	}
	return items, nil
}

type autocommitQuery func(sqlowner.AutocommitSeal, context.Context, string, ...any) (*database.Rows, error)

func autocommit(ctx context.Context, p writePlan, source clock.Clock, send autocommitQuery, scan func(database.Row) error) (error, bool) {
	p, err := p.conventions(ctx, source)
	if err != nil {
		return err, false
	}
	s, err := p.compile()
	if err != nil {
		return err, false
	}
	rows, err := send(sqlowner.Autocommit(), s.Context(ctx), s.SQLText, s.Bindings...)
	if err != nil {
		return err, false
	}
	err = returningN(ctx, rows, scan, p.returnCount(), p.bounded)
	if err != nil {
		return inspectOutcome(err, true)
	}
	return nil, false
}
func inspectOutcome(err error, autocommitted bool) (error, bool) {
	reconcile := true
	var outcome database.Outcome
	inspection := callback.Isolated("inspect orm write outcome", func() error {
		found, present, complete := errorgraph.As[*database.Error](err)
		if complete && present && found != nil {
			outcome = found.Outcome()
		}
		if !autocommitted {
			reconcile = !complete || present && (found == nil || outcome == database.Unknown || outcome == database.Committed)
		}
		return nil
	})
	if inspection != nil {
		err = errors.Join(err, inspection)
		outcome = ""
	}
	if autocommitted {
		switch outcome {
		case database.NoCommit, database.RolledBack:
			return err, false
		case database.Unknown, database.Committed:
			return err, true
		}
		return database.FoundryStatementUnknown(sqlowner.Outcomes(), "model write", err), true
	}
	return err, reconcile
}

func writeContext(ctx context.Context, writer database.Transactor) error {
	if ctx == nil || writer == nil {
		return invalid("write requires a context and transactor")
	}
	return ctx.Err()
}

// Restore clears the deletion instant of one deleted key.
func (t Table[M, K]) Restore(ctx context.Context, tx database.Transactor, key K) (M, error) {
	return writeOne[M](ctx, tx, t.Query().Where(t.Key().Eq(key)).state, querycore.RestoreModel, nil)
}

// ForceDelete physically deletes one active key. Use the package ForceDelete function for other visibility.
func (t Table[M, K]) ForceDelete(ctx context.Context, tx database.Transactor, key K) (M, error) {
	return writeOne[M](ctx, tx, t.Query().Where(t.Key().Eq(key)).state, querycore.ForceDeleteModel, nil)
}

// ForceDeleteAll physically deletes matching rows within the selected scope.
func (q Query[M]) ForceDeleteAll(ctx context.Context, tx database.Transactor) (int64, error) {
	return writeAll[M](ctx, tx, q.state, querycore.ForceDeleteModel, nil)
}

func (p writePlan) mutated() bool {
	return len(p.query.table.mutators) > 0 && p.kind.SqlKind() != querycore.DeleteModel
}
func (p writePlan) mutate(ctx context.Context) (writePlan, error) {
	if !p.mutated() {
		return p, nil
	}
	if p.batch != nil {
		rows := make([][]querycore.MutationAssignment, len(p.batch))
		for i, row := range p.batch {
			var err error
			rows[i], err = querycore.MutateAssignments(ctx, row, p.query.table.mutators)
			if err != nil {
				return p, err
			}
		}
		p.batch = rows
		return p, nil
	}
	var err error
	p.assignments, err = querycore.MutateAssignments(ctx, p.assignments, p.query.table.mutators)
	return p, err
}
