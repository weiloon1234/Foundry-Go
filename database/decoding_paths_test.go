package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONScalarDecodingRedacts(t *testing.T) {
	type row struct {
		ID      int64
		Payload value.JSON[string]
	}
	for _, api := range []string{"query"} {
		t.Run(api, func(t *testing.T) {
			private := &privateDecodePathError{}
			c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", private }).WithParameterType(codec.TypeText)
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				return &resultRows{state: state, values: [][]driver.Value{{"private"}}}, nil
			}
			db := open(t, state, nil)
			var err error
			{
				source := query.ForModel(query.Define("documents", "id", []query.Column{{Name: "id"}, {Name: "payload"}}, func(database.Row) (row, error) { return row{}, nil }))
				field := query.NewJSONField[row, value.JSON[string]]("documents", "payload", codec.JSON[string]())
				result, e := query.SelectValue(source, query.JSONScalar(query.JSONRoot(field), c).Value()).All(t.Context(), db)
				if len(result) != 0 {
					t.Fatal("partial scalar results published")
				}
				err = e
			}
			assertDecodePathCause(t, err, private)
			if state.rowsClosed.Load() != 1 {
				t.Fatal("scalar failure leaked rows")
			}
		})
	}
}

func TestRelationBatchDecodingRedacts(t *testing.T) {
	type row struct {
		ID       int64
		ParentID int64
		Name     string
		Children relation.Many[row]
	}
	for _, api := range []string{"query"} {
		t.Run(api, func(t *testing.T) {
			private := &privateDecodePathError{}
			c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", private }).WithParameterType(codec.TypeText)
			state := &driverState{}
			state.query = func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				return &resultRows{state: state, columns: []string{"id", "parent_id", "name"}, values: [][]driver.Value{{int64(2), int64(1), "private"}}}, nil
			}
			db := open(t, state, nil)
			parents := []row{{ID: 1}}
			var result []row
			var err error
			{
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
			assertDecodePathCause(t, err, private)
			if len(result) != 0 || parents[0].Children.IsLoaded() || state.rowsClosed.Load() != 1 {
				t.Fatal("failed relation batch published partial models or leaked rows")
			}
		})
	}
}

type privateDecodePathError struct{ calls int }

func (e *privateDecodePathError) Error() string { e.calls++; panic("private decoding error") }
func (e *privateDecodePathError) Format(fmt.State, rune) {
	e.calls++
	panic("private decoding formatter")
}
func assertDecodePathCause(t *testing.T, err error, cause *privateDecodePathError) {
	t.Helper()
	var retained *privateDecodePathError
	if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("decoding cause lost")
	}
	_ = fmt.Sprintf("%v %+v %#v", err, err, err)
	if cause.calls != 0 {
		t.Fatal("decoding cause formatted")
	}
}
