package jsonqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestPostgresJSONInvalidStoredShapeDiscardsCollectedRows(t *testing.T) {
	runJSON(t, func(tx *database.Tx, _ []jsonqueries.Document) error {
		// An external writer can bypass the Go schema. Hydration must still validate.
		if _, err := tx.Exec(t.Context(), `UPDATE json_documents SET settings='{"unknown":true}'::jsonb WHERE id=3`); err != nil {
			return err
		}
		q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
		if rows, err := orm.All(t.Context(), tx, q.OrderBy(orm.KeyAsc(f.Key()))); err == nil || rows != nil {
			t.Fatal("bad shape returned partial models", rows, err)
		}
		if rows, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.OrderBy(orm.KeyAsc(f.Key()))), expr.Row(expr.Value(orm.JSONExpr(f.Settings))).Value())).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("bad shape returned partial JSON values", rows, err)
		}
		// A decode error is not a SQL transaction error; subsequent valid reads work.
		if row, err := orm.RequireKey(t.Context(), tx, jsonqueries.Documents.Table, 1); err != nil || row.ID != 1 {
			t.Fatal(row, err)
		}
		return nil
	})
}
