package database_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONScalarDecodingRedactsInORM(t *testing.T) {
	type row struct {
		ID      int64
		Payload value.JSON[string]
	}
	type table struct {
		orm.Table[row, int64]
		Payload orm.JSON[row, string]
	}
	fields := orm.Define("documents", func(b orm.Builder[row], m *row) table { return table{orm.TableOf(b, &m.ID), orm.JSONOf(b, &m.Payload)} })
	t.Run("orm", func(t *testing.T) {
		private := &privateSQLError{}
		c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", private }).WithParameterType(codec.TypeText)
		state := &driverState{}
		state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return &resultRows{state: state, values: [][]driver.Value{{"private"}}}, nil
		}
		db := open(t, state, nil)
		selected := expr.JSONScalar(expr.JSONRoot(expr.Value(fields.Payload)), c)
		result, err := rows.ReadScalar(rows.SelectScalar(rows.From(fields.Query()), selected.Value())).All(t.Context(), db)
		if len(result) != 0 {
			t.Fatal("partial scalar results published")
		}
		assertSQLCause(t, err, private)
		if state.rowsClosed.Load() != 1 {
			t.Fatal("scalar failure leaked rows")
		}
	})
}

func TestRelationBatchDecodingRedactsInORM(t *testing.T) {
	type row struct {
		ID       int64
		ParentID int64
		Name     string
		Children relation.Many[row]
	}
	type table struct {
		orm.Table[row, int64]
		ParentID orm.Number[row, int64]
		Name     orm.Column[row, string]
		Children orm.Many[row, row]
	}
	t.Run("orm", func(t *testing.T) {
		private := &privateSQLError{}
		c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", private }).WithParameterType(codec.TypeText)
		state := &driverState{}
		state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return &resultRows{state: state, columns: []string{"id", "parent_id", "name"}, values: [][]driver.Value{{int64(2), int64(1), "private"}}}, nil
		}
		db := open(t, state, nil)
		parents := []row{{ID: 1}}
		fields := orm.Define("records", func(b orm.Builder[row], m *row) table {
			return table{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.ColumnOf(b, &m.Name, orm.Codec(c)), orm.ManyOf(b, &m.Children)}
		})
		result, err := orm.LoadFrom(t.Context(), db, fields.Query().With(orm.HasManyOf(fields.Children, fields.Key(), fields.ParentID)), parents)
		assertSQLCause(t, err, private)
		if len(result) != 0 || parents[0].Children.IsLoaded() || state.rowsClosed.Load() != 1 {
			t.Fatal("failed relation batch published partial models or leaked rows")
		}
	})
}
