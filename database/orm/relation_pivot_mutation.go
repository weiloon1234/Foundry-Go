package orm

import (
	"context"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

func relationPivotDefaults(defaults []querycore.MutationAssignment, c *columnCore, raw any) ([]querycore.MutationAssignment, error) {
	if c == nil || c.storedAssignment == nil {
		return nil, invalid("pivot key has no assignment codec")
	}
	v, err := c.storedAssignment(raw)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(defaults)
	return append(out, querycore.MutationAssignment{Field: querycore.FieldRef{Table: c.owner.model.Table, Column: c.metadata.Name}, AssignmentValue: v}), nil
}
func createPivots(ctx context.Context, tx *database.Tx, r *relationPlan, source pivotWriteKey, targets []pivotWriteKey, template writePlan, hooked bool, check func([]unsafe.Pointer) error) ([]unsafe.Pointer, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	plans := make([]writePlan, len(targets))
	table := r.join.pivot.table
	for i, target := range targets {
		defaults, err := relationPivotDefaults(r.join.defaults, pivotColumn(table, r.join.first.Column), source.raw)
		if err != nil {
			return nil, err
		}
		defaults, err = relationPivotDefaults(defaults, pivotColumn(table, r.join.second.Column), target.raw)
		if err != nil {
			return nil, err
		}
		p := template
		p.assignments = slices.Clone(p.assignments)
		for _, a := range defaults {
			if !p.present(a.Field.Column) {
				c := pivotColumn(table, a.Field.Column)
				// Match generated FoundryCreateMutation: stored defaults cannot
				// invent a distinct input, even when a hook could supply one later.
				if c.mutator.Input != nil && c.mutator.Input != c.mutator.Output {
					return nil, invalid("relation key with a distinct mutation input requires an explicit assignment")
				}
				p.assignments = append(p.assignments, a)
			}
		}
		plans[i] = p
	}
	var created []unsafe.Pointer
	if hooked {
		for _, p := range plans {
			rows, err := mutatePivotPlan(ctx, tx, p, true)
			if err != nil {
				return nil, err
			}
			created = append(created, rows...)
		}
	} else {
		p := template
		p.batch = make([][]querycore.MutationAssignment, len(plans))
		for i, plan := range plans {
			p.batch[i] = plan.assignments
		}
		var err error
		created, err = mutatePivotPlan(ctx, tx, p, false)
		if err != nil {
			return nil, err
		}
	}
	return created, check(created)
}

func pivotIdentities(table *tableCore, rows []unsafe.Pointer) ([]pivotWriteKey, error) {
	primary := pivotColumn(table, table.model.Primary)
	keys := make([]pivotWriteKey, len(rows))
	seen := map[sqlvalue.Value]bool{}
	for i, ptr := range rows {
		k, err := pivotKey(primary, ptr)
		if err != nil {
			return nil, err
		}
		if seen[k.identity] {
			return nil, querycore.DatabaseFailure("pivot primary key", database.TooManyRows)
		}
		seen[k.identity] = true
		keys[i] = k
	}
	return keys, nil
}
func changePivots(ctx context.Context, tx *database.Tx, q queryState, kind querycore.MutationKind, update *writePlan, limit int, hooked bool) ([]unsafe.Pointer, error) {
	p, err := planWrite[struct{}](q, kind, nil, false, true)
	if err != nil {
		return nil, err
	}
	if update != nil {
		p.assignments = update.assignments
		p.adjust = update.adjust
		p.bounded = update.bounded
		p.boundIndex = len(p.query.ast.Predicates) + update.boundIndex
		// Retain numeric adjustment bounds captured by the typed assignment.
		p.query.ast.Predicates = append(slices.Clone(p.query.ast.Predicates), update.query.ast.Predicates...)
	}
	if !hooked {
		p.many = true
		p.returnLimit = limit
		if querycore.HasEncryptedAssignments(p.assignments) {
			return nil, querycore.SetEncryptedError()
		}
		return mutatePivotPlan(ctx, tx, p, false)
	}
	candidates, err := pivotRead(ctx, tx, p.query, querycore.LockUpdate, limit, true)
	if err != nil {
		return nil, err
	}
	keys, err := pivotIdentities(q.table, candidates)
	if err != nil {
		return nil, err
	}
	primary := pivotColumn(q.table, q.table.model.Primary)
	var result []unsafe.Pointer
	for _, key := range keys {
		item := p
		item.query = pivotWhere(p.query, pivotKeyPredicate(primary, []pivotWriteKey{key}, false))
		rows, e := mutatePivotPlan(ctx, tx, item, true)
		if e != nil {
			return nil, e
		}
		result = append(result, rows...)
	}
	return result, nil
}

// Every pivot uses the ordinary write pipeline; the outer operation owns the
// transaction/savepoint and publishes results only after its outcome is known.
func mutatePivotPlan(ctx context.Context, tx *database.Tx, p writePlan, hooked bool) ([]unsafe.Pointer, error) {
	p.query.ast.ScopeContext = ctx
	candidate := reflect.New(p.query.table.typ).UnsafePointer()
	callbacks := modelCallbacks(p.query.table, candidate)
	prepared, err := precompileWrite(ctx, p, hooked)
	if err != nil {
		return nil, err
	}
	var out []unsafe.Pointer
	err = writeInTransaction(ctx, tx, p, prepared, hooked, func(row database.Row) error {
		ptr := candidate
		if !hooked {
			ptr = reflect.New(p.query.table.typ).UnsafePointer()
		}
		if err := hydrate(p.query.table, row, ptr); err != nil {
			return err
		}
		out = append(out, ptr)
		return nil
	}, nil, callbacks)
	return out, err
}

func requirePivotLinks(ctx context.Context, tx *database.Tx, r *relationPlan, source pivotWriteKey, created []unsafe.Pointer) error {
	table := r.join.pivot.table
	keys, err := pivotIdentities(table, created)
	if err != nil {
		return err
	}
	node, err := relationSelect(r, ctx)
	if err != nil {
		return err
	}
	filters := []querycore.Expression{
		pivotKeyPredicate(pivotColumn(table, table.model.Primary), keys, true),
		pivotKeyPredicate(pivotColumn(table, r.join.first.Column), []pivotWriteKey{source}, false),
	}
	node.Predicates = append(node.Predicates, querycore.Requalify(querycore.Junction{Children: filters}, querycore.RelationPivotAlias))
	node.Limit = value.Set(len(created) + 1)
	statement, err := querycore.CompileRead(node, ctx, true, nil, true)
	if err != nil {
		return err
	}
	statement = statement.Envelope(`SELECT COUNT(*) FROM (`, `) AS "foundry_links"`)
	var matched int64
	if err = database.ScanOne(statement.Context(ctx), tx, statement.SQLText, statement.Bindings, codec.Signed[int64]().Scan(&matched)); err != nil {
		return err
	}
	if matched != int64(len(created)) {
		return invalid("stored pivot does not satisfy relation endpoints and filters")
	}
	return nil
}

// Single attachment rechecks the original identities and both scoped endpoints
// after lifecycle callbacks, correlating the current source key to the stored pivot.
func requirePivotEndpoints(ctx context.Context, tx *database.Tx, r *relationPlan, source, target unsafe.Pointer, created []unsafe.Pointer) error {
	keys, err := pivotIdentities(r.join.pivot.table, created)
	if err != nil {
		return err
	}
	sourcePrimary := r.slot.owner.primary
	sourceID, err := pivotKey(sourcePrimary, source)
	if err != nil {
		return err
	}
	targetPrimary := r.target.primary
	targetID, err := pivotKey(targetPrimary, target)
	if err != nil {
		return err
	}
	scoped := copyJoined(r)
	scoped.query = pivotWhere(scoped.query, pivotKeyPredicate(targetPrimary, []pivotWriteKey{targetID}, false))
	scoped.query.ast.ScopeContext = ctx
	scoped.join.pivot = pivotWhere(scoped.join.pivot, pivotKeyPredicate(scoped.join.pivot.table.primary, keys, false))
	scoped.join.pivot.ast.ScopeContext = ctx
	q := pivotWhere(newQuery(r.slot.owner), pivotKeyPredicate(sourcePrimary, []pivotWriteKey{sourceID}, false), querycore.SubqueryPredicate{Query: relationExistsNode(scoped)})
	q.ast.ScopeContext = ctx
	statement, err := compileRead(q, querycore.ReadExists)
	if err != nil {
		return err
	}
	var matched bool
	if err = database.ScanOne(statement.Context(ctx), tx, statement.SQLText, statement.Bindings, codec.Bool[bool]().Scan(&matched)); err != nil {
		return err
	}
	if !matched {
		return invalid("stored pivot does not satisfy relation endpoints and filters")
	}
	return nil
}
