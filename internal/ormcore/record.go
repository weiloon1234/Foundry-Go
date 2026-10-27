package ormcore

import (
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Record keeps an ordered complete decoder bound to its declaration and Go type.
// Scan receives memory only after the typed adapter validates Type.
type RecordField struct {
	Name      string
	Sensitive bool
	Get       func(unsafe.Pointer) (driver.Value, error)
	Decode    func(any) (driver.Value, error)
}

type Record struct {
	Primary   string // Model-owned primary column; empty for declared projections.
	Fields    func() []RecordField
	Type      reflect.Type
	Owner     any
	Columns   []querycore.Column
	Scan      func(database.Row, unsafe.Pointer) error
	Callbacks func(database.Executor, func() (unsafe.Pointer, int)) *ReadCallbacks
	Checks    []func() error
	Err       error
}

// RecordSource returns SQL, result metadata and executor kind in one operation.
type RecordSource struct {
	Source      Source
	Record      Record
	Transaction bool
	Direct      bool
}

func ValidateRecord(r Record, typ reflect.Type) error {
	if r.Type != typ || typ == nil || r.Owner == nil || r.Scan == nil || len(r.Columns) == 0 || len(r.Columns) > querycore.MaxExpressionNodes {
		return safeerror.Join("operation failed", r.Err, Invalid("record requires its complete declared result and decoder"))
	}
	return r.Err
}

func ValidateRecordSource(r RecordSource, typ reflect.Type, transaction bool) error {
	err := safeerror.Join("operation failed", r.Source.Err, ValidateRecord(r.Record, typ))
	if r.Transaction != transaction {
		err = safeerror.Join("operation failed", err, Invalid("record executor kind does not match its source"))
	}
	if len(r.Source.Node.Selections) != len(r.Record.Columns) {
		return safeerror.Join("operation failed", err, Invalid("record selection is incomplete"))
	}
	for i, item := range r.Source.Node.Selections {
		name := item.Alias
		if name == "" {
			if f, ok := item.Expression.(querycore.FieldRef); ok {
				name = f.Column
			}
		}
		if name != r.Record.Columns[i].Name {
			err = safeerror.Join("operation failed", err, Invalid("record selection order does not match its decoder"))
		}
	}
	return err
}

func SelectRecord(source Source, scope Scope, record Record, typ reflect.Type) RecordSource {
	source.Err = safeerror.Join("operation failed", source.Err, scope.Err, ValidateRecord(record, typ), CheckReferences(source, []Reference{scope.Reference}))
	if scope.Owner != record.Owner {
		source.Err = safeerror.Join("operation failed", source.Err, Invalid("record scope belongs to another declaration"))
	}
	node, err := querycore.SelectRecordNode(source.Node, scope.Table, record.Columns, record.Scan != nil)
	source.Node = node
	source.Err = safeerror.Join("operation failed", source.Err, err)
	source.Checks = AppendChecks(source.Checks, record.Checks...)
	return RecordSource{Source: source, Record: record}
}

func AliasRecord(r RecordSource, alias string, typ, scope reflect.Type, transaction bool) Source {
	err := ValidateRecordSource(r, typ, transaction)
	node := querycore.AliasNode(r.Source.Node, slices.Clone(r.Record.Columns), r.Direct, alias, scope, err)
	return Source{Scope: scope, Node: node.Node, Aliases: node.Aliases, Owners: map[string]any{alias: r.Record.Owner}, Err: node.Err, Checks: AppendChecks(r.Source.Checks, r.Record.Checks...)}
}

func DirectRecord(source Source) bool {
	node := source.Node
	node.Source = querycore.TableSource{}
	node.Selections = nil
	// ModelSelect allocates empty order/predicate slices. Their SQL meaning is
	// the same as nil; every populated clause must still retain its boundary.
	if len(node.Orders) == 0 {
		node.Orders = nil
	}
	if len(node.Predicates) == 0 {
		node.Predicates = nil
	}
	return reflect.ValueOf(node).IsZero()
}

func ReadRecord(ctx context.Context, ex database.Executor, r RecordSource, typ reflect.Type, transaction bool, scan func(database.Row) error, batch func() (unsafe.Pointer, int)) error {
	err := ValidateRecordSource(r, typ, transaction)
	var callbacks *ReadCallbacks
	if err == nil && r.Record.Callbacks != nil {
		callbacks = r.Record.Callbacks(ex, batch)
	}
	return readComplete(ctx, ex, r.Source.Node, err, AppendChecks(r.Source.Checks, r.Record.Checks...), scan, callbacks, transaction)
}
