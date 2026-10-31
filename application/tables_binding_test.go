package application_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateBindingError struct{ formatted int }

func (e *privateBindingError) Error() string { e.formatted++; return "private-binding-value" }
func (e *privateBindingError) Format(s fmt.State, _ rune) {
	e.formatted++
	fmt.Fprint(s, "private-binding-formatter")
}

type bindingExecutor struct{ calls int }

func (e *bindingExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	e.calls++
	return database.Result{}, errors.New("unexpected execution")
}
func (e *bindingExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	e.calls++
	return nil, errors.New("unexpected execution")
}

func assertPrivateBindingError(t *testing.T, err error, cause *privateBindingError) {
	t.Helper()
	var retained *privateBindingError
	if !errors.Is(err, cause) || errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("binding error lost its cause or classification")
	}
	for _, message := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Errorf("startup: %w", err).Error()} {
		if strings.Contains(message, "private-binding") {
			t.Fatal("binding diagnostic disclosed private data")
		}
	}
	if cause.formatted != 0 {
		t.Fatal("binding cause was formatted")
	}
}

func TestTablesRedactCompiledAndCapturedBindingErrors(t *testing.T) {
	type row struct {
		ID       int64
		ParentID int64
		Name     string
		Children relation.Many[row]
		Count    relation.Value[int64]
	}
	type table struct {
		orm.Table[row, int64]
		ParentID orm.Number[row, int64]
		Name     orm.Column[row, string]
		Children orm.Many[row, row]
		Count    orm.Value[row, int64]
	}
	for _, kind := range []string{"codec", "validator"} {
		t.Run(kind, func(t *testing.T) {
			cause := &privateBindingError{}
			c := codec.New(func(string) (driver.Value, error) { return nil, cause }, func(any) (string, error) { return "", nil }).WithParameterType(codec.TypeText)
			if kind == "validator" {
				c = codec.String[string]().Validated(func(string) error { return cause })
			}
			fields := orm.Define("binding_rows", func(b orm.Builder[row], m *row) table {
				return table{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.ColumnOf(b, &m.Name, orm.Codec(c)), orm.ManyOf(b, &m.Children), orm.ValueOf(b, &m.Count)}
			})
			if err := orm.Check(fields.Table); err != nil {
				t.Fatal(err)
			}
			if cause.formatted != 0 {
				t.Fatal("table structure validation executed binding")
			}
			direct := orm.HasManyOf(fields.Children, orm.KeyExpr(fields.Key()), orm.NumberExpr(fields.ParentID))
			through := orm.HasManyThroughOf(fields.Children, orm.KeyExpr(fields.Key()), orm.NumberExpr(fields.ParentID), orm.KeyExpr(fields.Key()), orm.NumberExpr(fields.ParentID)).Where(orm.ColumnEq(fields.Name, "private input"))
			filtered := fields.Count.Using(expr.Related(direct, expr.Filter(expr.Count[row](), expr.Eq(expr.Value(orm.ColumnExpr(fields.Name)), "private input"))))
			captured := fields.Count.Using(expr.Related(direct, expr.Filter(expr.Count[row](), expr.IsNull(expr.Param(orm.ColumnExpr(fields.Name), "private input")))))
			for _, tc := range []struct {
				name        string
				declaration orm.CheckedTable
				relation    orm.Relation[row]
			}{
				{"through_predicate", through, through},
				{"aggregate_filter", filtered, filtered},
				{"captured_parameter", captured, captured},
			} {
				t.Run(tc.name, func(t *testing.T) {
					assertPrivateBindingError(t, orm.Check(tc.declaration), cause)
					app, err := application.New(settings(), quiet(), application.WithTables(fields.Table, tc.declaration)).Build(t.Context())
					if app != nil {
						t.Fatal("invalid relation started application")
					}
					assertPrivateBindingError(t, err, cause)
					executor := &bindingExecutor{}
					_, err = orm.All(t.Context(), executor, fields.Query().With(tc.relation))
					assertPrivateBindingError(t, err, cause)
					if executor.calls != 0 {
						t.Fatal("invalid relation acquired database resources")
					}
				})
			}
			executor := &bindingExecutor{}
			_, err := orm.All(t.Context(), executor, fields.Query().Where(orm.ColumnEq(fields.Name, "private input")))
			assertPrivateBindingError(t, err, cause)
			if executor.calls != 0 {
				t.Fatal("invalid predicate acquired database resources")
			}
		})
	}
}
