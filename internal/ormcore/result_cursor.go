package ormcore

import (
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ResultCursor retains the boundary, output identity and the complete decoder.
type ResultCursor struct {
	Record RecordSource
	Orders []querycore.QueryOrder
	Unique []querycore.FieldRef
}

func NewResultCursor(r RecordSource, scope reflect.Type) ResultCursor {
	err := ValidateRecordSource(r, r.Record.Type, false)
	node, nodeErr := querycore.ResultCursorNode(r.Source.Node, r.Record.Columns, r.Record.Scan != nil)
	r.Source = Source{Node: node, Scope: scope, Owners: map[string]any{node.Source.Name(): r.Record.Owner}, Err: safeerror.Join("operation failed", err, nodeErr), Checks: AppendChecks(r.Source.Checks, r.Record.Checks...)}
	r.Direct = false
	return ResultCursor{Record: r}
}

func CursorOrder(q ResultCursor, orders []Order) ResultCursor {
	q.Orders = slices.Clone(q.Orders)
	for _, o := range orders {
		q.Record.Source.Err = safeerror.Join("operation failed", q.Record.Source.Err, o.Err, CheckReferences(q.Record.Source, o.Refs))
		field, ok := o.Node.Expression.(querycore.FieldRef)
		if !ok || o.Node.Nulls != querycore.NullsDefault {
			q.Record.Source.Err = safeerror.Join("operation failed", q.Record.Source.Err, Invalid("cursor ordering requires plain output fields with default NULL placement"))
		}
		q.Orders = append(q.Orders, querycore.QueryOrder{Field: field, Descending: o.Node.Descending})
	}
	return q
}
func CursorUnique(q ResultCursor, keys []Expression) ResultCursor {
	q.Unique = make([]querycore.FieldRef, len(keys))
	for i, k := range keys {
		q.Record.Source.Err = safeerror.Join("operation failed", q.Record.Source.Err, k.Err, CheckReferences(q.Record.Source, k.Refs))
		f, ok := k.Node.(querycore.FieldRef)
		if !ok {
			q.Record.Source.Err = safeerror.Join("operation failed", q.Record.Source.Err, Invalid("cursor identity requires output fields"))
		}
		q.Unique[i] = f
	}
	return q
}

func canonicalCursor(q ResultCursor) (ResultCursor, []RecordField, []bool, error) {
	if err := ValidateRecordSource(q.Record, q.Record.Record.Type, false); err != nil {
		return q, nil, nil, err
	}
	orders, err := querycore.ResultCursorOrders(q.Record.Source.Node, q.Record.Record.Columns, q.Orders, q.Unique)
	if err != nil {
		return q, nil, nil, err
	}
	if q.Record.Record.Fields == nil {
		return q, nil, nil, Invalid("cursor result requires declared field getters")
	}

	all := q.Record.Record.Fields()
	metadata := make([]querycore.ResultCursorField, len(all))
	for i, f := range all {
		metadata[i] = querycore.ResultCursorField{Name: f.Name, Sensitive: f.Sensitive, Valid: f.Get != nil && f.Decode != nil}
	}
	indices, required, err := querycore.ResultCursorFields(q.Record.Source.Node, q.Record.Record.Columns, orders, metadata)
	if err != nil {
		return q, nil, nil, err
	}
	fields := make([]RecordField, len(indices))
	for i, index := range indices {
		fields[i] = all[index]
	}

	q.Orders = orders
	q.Record.Source.Node.Orders = querycore.OrderNodes(orders)
	return q, fields, required, nil
}

// CursorPlan owns codecs and query identity for one validated navigation request.
type CursorPlan struct {
	Record RecordSource
	Fields []RecordField
	Scope  string
}

func PlanResultCursor(ctx context.Context, q ResultCursor, size int, token string, present, backward bool) (CursorPlan, error) {
	q, fields, required, err := canonicalCursor(q)
	if err != nil {
		return CursorPlan{}, err
	}
	for _, check := range q.Record.Source.Checks {
		if check != nil {
			err = safeerror.Join("operation failed", err, check())
		}
	}
	statement, err := querycore.CompileRead(q.Record.Source.Node, ctx, false, err, true)
	if err != nil {
		return CursorPlan{}, err
	}
	identity := []string{"result-cursor"}
	for _, key := range q.Unique {
		identity = append(identity, key.Column)
	}
	typ := q.Record.Record.Type
	scope, err := querycore.CursorStatementScopeFor(typ.PkgPath()+"/"+typ.String(), statement, identity)
	if err != nil {
		return CursorPlan{}, err
	}
	adapters := make([]querycore.CursorField, len(fields))
	for i, f := range fields {
		adapters[i] = querycore.CursorField{Sensitive: f.Sensitive, Decode: f.Decode}
	}
	boundary, err := querycore.ReadCursorBoundary(token, present, backward, scope, adapters, required)
	if err != nil {
		return CursorPlan{}, err
	}
	orders := querycore.CursorOrders(q.Orders, backward)
	q.Record.Source.Node.Orders = querycore.OrderNodes(orders)
	if present {
		q.Record.Source.Node.Predicates = append(slices.Clone(q.Record.Source.Node.Predicates), querycore.CursorPredicate(orders, boundary.Keys, func(querycore.QueryOrder) bool { return true }))
	}
	q.Record.Source.Node.Limit = value.Set(size + 1)
	return CursorPlan{Record: q.Record, Fields: fields, Scope: scope}, nil
}
func (p CursorPlan) Token(base unsafe.Pointer) (string, error) {
	return querycore.BuildCursor(p.Scope, len(p.Fields), func(i int) (driver.Value, error) { return p.Fields[i].Get(base) })
}

// CompileResultCursor plans canonical ordering without a page boundary or lookahead.
func CompileResultCursor(ctx context.Context, q ResultCursor) (querycore.Statement, error) {
	canonical, _, _, err := canonicalCursor(q)
	if err != nil {
		return querycore.Statement{}, err
	}
	return CompileRecord(ctx, canonical.Record)
}
