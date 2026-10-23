// Package ormcore shares sealed metadata between orm, expr and rows.
package ormcore

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Source is a validated model/alias/join input without a result decoder.
type Source struct {
	Scope   reflect.Type
	Node    querycore.SelectNode
	Owners  map[string]any
	Aliases map[string]reflect.Type
	Err     error
	Checks  []func() error
}

// Reference retains runtime declaration ownership after static scope checking.
type Reference struct {
	Scope reflect.Type
	Table string
	Owner any
}

// Scope identifies one preserved model inside a query source.
type Scope struct {
	Reference
	Err error
}

// Column retains typed closures without a heavy codec field.
type Column[V any] struct {
	Reference
	Field    querycore.FieldRef
	Kind     codec.ParameterType
	Nullable bool
	Bind     func(V) (driver.Value, error)
	Clone    func(V) V
	Scan     func(*V) any
	Err      error
}

// Expression carries SQL and the referenced declarations for ownership checks.
type Expression struct {
	Node querycore.ValueExpression
	Refs []Reference
	// ReferenceFree marks a constructed expression with no SQL column dependencies.
	ReferenceFree bool
	Err           error
}

// Predicate carries a scoped SQL condition.
type Predicate struct {
	Node querycore.Expression
	Refs []Reference
	// ReferenceFree marks a constructed expression with no SQL column dependencies.
	ReferenceFree bool
	Err           error
}

// Order carries a scoped ordering.
type Order struct {
	Node querycore.OrderNode
	Refs []Reference
	// ReferenceFree marks a constructed expression with no SQL column dependencies.
	ReferenceFree bool
	Err           error
}

func Invalid(message string) error { return fault.New(fault.Invalid, message) }
func CheckReferences(s Source, refs []Reference) error {
	if len(refs) == 0 || len(s.Owners) == 0 {
		return Invalid("expression requires an owned source")
	}
	if len(refs) > querycore.MaxExpressionNodes {
		return Invalid("expression references exceed their resource bound")
	}
	for _, r := range refs {
		if r.Scope != nil {
			if r.Scope != s.Scope {
				return Invalid("expression belongs to a different source scope")
			}
			if r.Table == "" && r.Owner == nil {
				continue
			}
		}
		if r.Owner == nil || !reflect.ValueOf(r.Owner).Comparable() || s.Owners[r.Table] != r.Owner {
			return Invalid("expression belongs to a different source declaration")
		}
	}
	return nil
}

// CheckReferenceSet distinguishes constructed constants from missing ownership.
func CheckReferenceSet(s Source, refs []Reference, referenceFree bool) error {
	if len(refs) == 0 && referenceFree {
		if len(s.Owners) == 0 {
			return Invalid("expression requires a declared source")
		}
		return nil
	}
	return CheckReferences(s, refs)
}

// NilDescriptor handles typed nils at public declaration boundaries.
func NilDescriptor(v any) bool {
	return v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()
}

// RowSource is the in-memory/testable shape of database.Rows.
type RowSource interface {
	database.Row
	Next() bool
	Err() error
	Close() error
}

// Consume owns close/error handling for model and projected reads.
func Consume(ctx context.Context, rows RowSource, scan func(database.Row) error) (err error) {
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := scan(rows); err != nil {
			return err
		}
	}
	return errors.Join(ctx.Err(), rows.Err())
}

// Read centralizes projection/scalar execution and discards unfinished rows.
func Read(ctx context.Context, ex database.Executor, node querycore.SelectNode, planError error, checks []func() error, scan func(database.Row) error) (err error) {
	return ReadComplete(ctx, ex, node, planError, checks, scan, nil)
}

// ReadComplete retains complete-model callback ownership through row closure.
func ReadComplete(ctx context.Context, ex database.Executor, node querycore.SelectNode, planError error, checks []func() error, scan func(database.Row) error, callbacks *ReadCallbacks) (err error) {
	return readComplete(ctx, ex, node, planError, checks, scan, callbacks, false)
}

func readComplete(ctx context.Context, ex database.Executor, node querycore.SelectNode, planError error, checks []func() error, scan func(database.Row) error, callbacks *ReadCallbacks, transaction bool) (err error) {
	if ctx == nil || ex == nil {
		return Invalid("query execution requires a context and executor")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, check := range checks {
		if check != nil {
			planError = errors.Join(planError, check())
		}
	}
	if transaction {
		if tx, ok := ex.(*database.Tx); !ok || tx == nil {
			return Invalid("record read requires a transaction")
		}
	}
	statement, err := querycore.CompileRead(node, ctx, transaction, planError, scan != nil)
	if err != nil {
		return err
	}
	rows, err := database.ReadQuery(statement.Context(ctx), ex, statement.SQLText, statement.Bindings...)
	if err != nil {
		return err
	}
	return Collect(ctx, rows, scan, callbacks)
}

// AppendChecks snapshots deferred declaration checks without specializing slice
// helpers in every generic join/projection consumer.
func AppendChecks(first []func() error, next ...func() error) []func() error {
	result := make([]func() error, len(first)+len(next))
	copy(result, first)
	copy(result[len(first):], next)
	return result
}
