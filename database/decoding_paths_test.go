package database_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONScalarDecodingRedactsInBothQueryAPIs(t *testing.T) {
	type row struct {
		ID      int64
		Payload value.JSON[string]
	}
	type table struct {
		orm.Table[row, int64]
		Payload orm.JSON[row, string]
	}
	fields := orm.Define("documents", func(b orm.Builder[row], m *row) table { return table{orm.TableOf(b, &m.ID), orm.JSONOf(b, &m.Payload)} })
	for _, api := range []string{"orm", "query"} {
		t.Run(api, func(t *testing.T) {
			private := &privateSQLError{}
			c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", private }).WithParameterType(codec.TypeText)
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				return &resultRows{state: state, values: [][]driver.Value{{"private"}}}, nil
			}
			db := open(t, state, nil)
			var err error
			if api == "orm" {
				selected := expr.JSONScalar(expr.JSONRoot(expr.Value(fields.Payload)), c)
				result, e := rows.ReadScalar(rows.SelectScalar(rows.From(fields.Query()), selected.Value())).All(t.Context(), db)
				if len(result) != 0 {
					t.Fatal("partial scalar results published")
				}
				err = e
			} else {
				source := query.ForModel(query.Define("documents", "id", []query.Column{{Name: "id"}, {Name: "payload"}}, func(database.Row) (row, error) { return row{}, nil }))
				field := query.NewJSONField[row, value.JSON[string]]("documents", "payload", codec.JSON[string]())
				result, e := query.SelectValue(source, query.JSONScalar(query.JSONRoot(field), c).Value()).All(t.Context(), db)
				if len(result) != 0 {
					t.Fatal("partial scalar results published")
				}
				err = e
			}
			assertSQLCause(t, err, private)
			if state.rowsClosed.Load() != 1 {
				t.Fatal("scalar failure leaked rows")
			}
		})
	}
}

func TestRelationBatchDecodingRedactsInBothQueryAPIs(t *testing.T) {
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
	for _, api := range []string{"orm", "query"} {
		t.Run(api, func(t *testing.T) {
			private := &privateSQLError{}
			c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", private }).WithParameterType(codec.TypeText)
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				return &resultRows{state: state, columns: []string{"id", "parent_id", "name"}, values: [][]driver.Value{{int64(2), int64(1), "private"}}}, nil
			}
			db := open(t, state, nil)
			parents := []row{{ID: 1}}
			var result []row
			var err error
			if api == "orm" {
				fields := orm.Define("records", func(b orm.Builder[row], m *row) table {
					return table{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.ParentID), orm.ColumnOf(b, &m.Name, orm.Codec(c)), orm.ManyOf(b, &m.Children)}
				})
				result, err = orm.Load(t.Context(), db, parents, orm.HasManyOf(fields.Children, fields.Key(), fields.ParentID))
			} else {
				source := query.ForModel(query.Define("records", "id", []query.Column{{Name: "id"}, {Name: "parent_id"}, {Name: "name"}}, func(r database.Row) (row, error) {
					var m row
					err := r.Scan(&m.ID, &m.ParentID, c.Scan(&m.Name))
					return m, err
				}, query.NewModelField("id", codec.Signed[int64](), func(m row) int64 { return m.ID }), query.NewModelField("parent_id", codec.Signed[int64](), func(m row) int64 { return m.ParentID }), query.NewModelField("name", c, func(m row) string { return m.Name })))
				id := query.NewScalarField[row, int64]("records", "id", codec.Signed[int64]())
				parent := query.NewScalarField[row, int64]("records", "parent_id", codec.Signed[int64]())
				children := query.HasMany(id, parent).Bind("Children", source, source, func(m row) relation.Many[row] { return m.Children }, func(m row, r relation.Many[row]) row { m.Children = r; return m })
				result, err = source.With(children).Load(t.Context(), db, parents)
			}
			assertSQLCause(t, err, private)
			if len(result) != 0 || parents[0].Children.IsLoaded() || state.rowsClosed.Load() != 1 {
				t.Fatal("failed relation batch published partial models or leaked rows")
			}
		})
	}
}
