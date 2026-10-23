package orm

import (
	"context"
	"database/sql/driver"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/value"
)

type pivotWriteMode uint8

const (
	pivotAttachOne pivotWriteMode = iota
	pivotAttach
	pivotDetachOne
	pivotForceDetach
	pivotSync
	pivotSyncKeep
	pivotToggle
	pivotDetach
	pivotDetachAll
	pivotUpdate
)

type pivotWriteResult struct{ attached, detached, updated []unsafe.Pointer }
type pivotWriteKey struct {
	raw      driver.Value
	identity sqlvalue.Value
}

func validateRelationWrite(p *relationPlan, mode pivotWriteMode) (int, error) {
	if err := validateRelation(p, nil); err != nil {
		return 0, err
	}
	if p.join == nil || p.join.hop || p.choice != 0 || p.aggregate != nil || p.morph != nil {
		return 0, invalid("relation writes require a concrete pivot relation")
	}
	if p.join.first == p.join.second {
		return 0, invalid("relation writes require distinct pivot keys")
	}
	if len(p.join.orders) > 0 || len(p.query.ast.Orders) > 0 || len(p.join.pivot.ast.Orders) > 0 || len(p.query.relations) > 0 || len(p.join.pivot.relations) > 0 || p.limits != nil || p.query.relationLimits != nil || p.join.pivot.relationLimits != nil {
		return 0, invalid("relation writes do not accept ordering or eager-loading options")
	}
	if mode == pivotForceDetach && !p.join.pivot.ast.HasSoftDeletes() {
		return 0, invalid("force detachment requires a soft-delete pivot")
	}
	limit := p.writeLimit
	if limit == 0 {
		limit = MaxRelationWriteRows
	}
	if limit < 1 || limit > MaxRelationWriteRows {
		return 0, invalid("invalid relation write row limit")
	}
	return limit, nil
}

func pivotColumn(t *tableCore, name string) *columnCore {
	for _, c := range t.columns {
		if c.metadata.Name == name {
			return c
		}
	}
	return nil
}
func pivotKey(c *columnCore, ptr unsafe.Pointer) (pivotWriteKey, error) {
	if c == nil || c.get == nil {
		return pivotWriteKey{}, invalid("relation key has no declared codec")
	}
	raw, err := c.get(ptr)
	if err != nil {
		return pivotWriteKey{}, err
	}
	if raw == nil {
		return pivotWriteKey{}, fault.New(fault.Missing, "relation writes require non-null keys")
	}
	id, err := querycore.RecordEqualityKey(c.kind, raw)
	return pivotWriteKey{raw: raw, identity: id}, err
}
func pivotKeyPredicate(c *columnCore, keys []pivotWriteKey, membership bool) querycore.Expression {
	values := make([]any, len(keys))
	for i, key := range keys {
		values[i] = key.raw
	}
	op := querycore.Equal
	if membership {
		op = querycore.In
	}
	return querycore.Comparison{Operand: querycore.FieldRef{Table: c.owner.model.Table, Column: c.metadata.Name}, Operator: op, Values: values, Kind: c.kind, Bind: func(raw any) (driver.Value, error) {
		v, err := c.storedAssignment(raw)
		if err != nil {
			return nil, err
		}
		return v.Bind()
	}}
}
func pivotWhere(q queryState, ps ...querycore.Expression) queryState {
	q.ast.Predicates = append(slices.Clone(q.ast.Predicates), ps...)
	return q
}
func pivotRead(ctx context.Context, tx *database.Tx, q queryState, lock querycore.LockStrength, limit int, ordered bool) ([]unsafe.Pointer, error) {
	q.ast.ScopeContext = ctx
	q.lock = lock
	q.ast.Limit = value.Set(limit + 1)
	if ordered {
		q.ast.Orders = []querycore.QueryOrder{{Field: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}}}
	}
	s, err := compileRead(q, querycore.ReadModels)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(s.Context(ctx), s.SQLText, s.Bindings...)
	if err != nil {
		return nil, err
	}
	var result []unsafe.Pointer
	err = consume(ctx, rows, func(row database.Row) error {
		if len(result) >= limit {
			if ordered {
				return invalid("relation write exceeds its row limit")
			}
			return database.NewError("relation write rows", database.TooManyRows)
		}
		ptr := reflect.New(q.table.typ).UnsafePointer()
		if err := hydrate(q.table, row, ptr); err != nil {
			return err
		}
		result = append(result, ptr)
		return nil
	})
	return result, err
}
func pivotSourceKey(ctx context.Context, tx *database.Tx, p *relationPlan, source unsafe.Pointer, exclusive bool) (pivotWriteKey, error) {
	q := newQuery(p.slot.owner)
	primary := pivotColumn(q.table, q.table.model.Primary)
	key, err := pivotKey(primary, source)
	if err != nil {
		return pivotWriteKey{}, err
	}
	lock := querycore.LockShare
	if exclusive {
		lock = querycore.LockNoKeyUpdate
	}
	rows, err := pivotRead(ctx, tx, pivotWhere(q, pivotKeyPredicate(primary, []pivotWriteKey{key}, false)), lock, 1, false)
	if err != nil {
		return pivotWriteKey{}, err
	}
	if len(rows) != 1 {
		return pivotWriteKey{}, database.NewError("relation source", database.NotFound)
	}
	return pivotKey(pivotColumn(q.table, p.local.Column), rows[0])
}
func pivotTargetKeys(ctx context.Context, tx *database.Tx, p *relationPlan, targets []unsafe.Pointer, single bool) ([]pivotWriteKey, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	primary := pivotColumn(p.target, p.target.model.Primary)
	var requested []pivotWriteKey
	seen := map[sqlvalue.Value]bool{}
	for _, ptr := range targets {
		k, err := pivotKey(primary, ptr)
		if err != nil {
			return nil, err
		}
		if !seen[k.identity] {
			seen[k.identity] = true
			requested = append(requested, k)
		}
	}
	q := pivotWhere(p.query, pivotKeyPredicate(primary, requested, !single))
	rows, err := pivotRead(ctx, tx, q, querycore.LockShare, len(requested), false)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(requested) {
		return nil, database.NewError("relation target", database.NotFound)
	}
	found := map[sqlvalue.Value]unsafe.Pointer{}
	for _, ptr := range rows {
		k, e := pivotKey(primary, ptr)
		if e != nil {
			return nil, e
		}
		if !seen[k.identity] || found[k.identity] != nil {
			return nil, database.NewError("relation target identity", database.TooManyRows)
		}
		found[k.identity] = ptr
	}
	foreign := pivotColumn(p.target, p.foreign.Column)
	var keys []pivotWriteKey
	distinct := map[sqlvalue.Value]bool{}
	for _, k := range requested {
		ptr := found[k.identity]
		if ptr == nil {
			return nil, database.NewError("relation target", database.NotFound)
		}
		key, e := pivotKey(foreign, ptr)
		if e != nil {
			return nil, e
		}
		if !distinct[key.identity] {
			distinct[key.identity] = true
			keys = append(keys, key)
		}
	}
	if foreign != primary {
		matches, e := pivotRead(ctx, tx, pivotWhere(p.query, pivotKeyPredicate(foreign, keys, !single)), querycore.LockShare, len(keys), false)
		if e != nil {
			return nil, e
		}
		if len(matches) != len(keys) {
			return nil, database.NewError("relation target key", database.TooManyRows)
		}
	}
	return keys, nil
}
func pivotLinks(p *relationPlan, source pivotWriteKey, targets []pivotWriteKey, all bool) queryState {
	q := pivotWhere(p.join.pivot, pivotKeyPredicate(pivotColumn(p.join.pivot.table, p.join.first.Column), []pivotWriteKey{source}, false))
	if !all {
		q = pivotWhere(q, pivotKeyPredicate(pivotColumn(q.table, p.join.second.Column), targets, true))
	}
	return q
}
func currentPivotKeys(ctx context.Context, tx *database.Tx, p *relationPlan, source pivotWriteKey, limit int) ([]pivotWriteKey, error) {
	rows, err := pivotRead(ctx, tx, pivotLinks(p, source, nil, true), querycore.LockUpdate, limit, true)
	if err != nil {
		return nil, err
	}
	var keys []pivotWriteKey
	seen := map[sqlvalue.Value]bool{}
	column := pivotColumn(p.join.pivot.table, p.join.second.Column)
	for _, ptr := range rows {
		k, e := pivotKey(column, ptr)
		if e != nil {
			return nil, e
		}
		if !seen[k.identity] {
			seen[k.identity] = true
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func writePivots(ctx context.Context, tx *database.Tx, p *relationPlan, source unsafe.Pointer, targets []unsafe.Pointer, create writePlan, update *writePlan, mode pivotWriteMode, limit int) (pivotWriteResult, error) {
	var out pivotWriteResult
	single := mode == pivotAttachOne || mode == pivotDetachOne || mode == pivotForceDetach
	sourceKey, err := pivotSourceKey(ctx, tx, p, source, !single && mode != pivotAttach)
	if err != nil {
		return out, err
	}
	desired, err := pivotTargetKeys(ctx, tx, p, targets, single)
	if err != nil {
		return out, err
	}
	table := p.join.pivot.table
	hooked := table.hooks != nil || table.observed(tx.Observers(), querycore.DeleteModel) || table.observed(tx.Observers(), querycore.InsertModel)
	if mode == pivotAttach || mode == pivotAttachOne {
		out.attached, err = createPivots(ctx, tx, p, sourceKey, desired, create, hooked, func(created []unsafe.Pointer) error {
			if single {
				return requirePivotEndpoints(ctx, tx, p, source, targets[0], created)
			}
			return requirePivotLinks(ctx, tx, p, sourceKey, created)
		})
		return out, err
	}
	if single {
		kind := querycore.DeleteModel
		if mode == pivotForceDetach {
			kind = querycore.ForceDeleteModel
		}
		selected := pivotLinks(p, sourceKey, desired, false)
		// Single-target operations retain equality predicates, matching the old API.
		selected = pivotWhere(p.join.pivot, pivotKeyPredicate(pivotColumn(table, p.join.first.Column), []pivotWriteKey{sourceKey}, false), pivotKeyPredicate(pivotColumn(table, p.join.second.Column), desired, false))
		out.detached, err = changePivots(ctx, tx, selected, kind, nil, limit, hooked)
		return out, err
	}
	current, err := currentPivotKeys(ctx, tx, p, sourceKey, limit)
	if err != nil {
		return out, err
	}
	linked, wanted := map[sqlvalue.Value]bool{}, map[sqlvalue.Value]bool{}
	for _, key := range current {
		linked[key.identity] = true
	}
	var attach, detach, keep []pivotWriteKey
	for _, key := range desired {
		wanted[key.identity] = true
		if linked[key.identity] {
			keep = append(keep, key)
		} else {
			attach = append(attach, key)
		}
	}
	switch mode {
	case pivotSync:
		for _, key := range current {
			if !wanted[key.identity] {
				detach = append(detach, key)
			}
		}
	case pivotToggle:
		detach, keep = keep, nil
	case pivotDetach:
		detach, attach, keep = keep, nil, nil
	case pivotDetachAll:
		attach = nil
	case pivotUpdate:
		if len(keep) == 0 {
			return out, database.NewError("pivot update", database.NotFound)
		}
		attach = nil
	}
	if mode == pivotDetachAll || len(detach) > 0 {
		out.detached, err = changePivots(ctx, tx, pivotLinks(p, sourceKey, detach, mode == pivotDetachAll), querycore.DeleteModel, nil, limit, hooked)
		if err != nil {
			return pivotWriteResult{}, err
		}
	}
	if update != nil && len(keep) > 0 {
		out.updated, err = changePivots(ctx, tx, pivotLinks(p, sourceKey, keep, false), querycore.UpdateModel, update, limit, hooked)
		if err != nil {
			return pivotWriteResult{}, err
		}
	}
	if len(attach) > 0 {
		out.attached, err = createPivots(ctx, tx, p, sourceKey, attach, create, hooked, func(created []unsafe.Pointer) error {
			return requirePivotLinks(ctx, tx, p, sourceKey, created)
		})
	}
	return out, err
}
