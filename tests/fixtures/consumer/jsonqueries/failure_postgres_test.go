package jsonqueries_test

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestPostgresJSONInvalidStoredShapeDiscardsCollectedRows(t *testing.T) {
	runJSON(t, func(tx *database.Tx, _ []jsonqueries.Document) error {
		// An external writer can bypass the Go schema. Hydration must still validate.
		if _, err := tx.Exec(t.Context(), `UPDATE json_documents SET settings='{"unknown":true}'::jsonb WHERE id=3`); err != nil {
			return err
		}
		q, f := jsonqueries.QueryJsonDocuments(), jsonqueries.DocumentFields()
		if rows, err := q.OrderBy(f.ID.Asc()).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("bad shape returned partial models", rows, err)
		}
		if rows, err := query.SelectValue(q.OrderBy(f.ID.Asc()), f.Settings.Value()).All(t.Context(), tx); err == nil || rows != nil {
			t.Fatal("bad shape returned partial JSON values", rows, err)
		}
		// A decode error is not a SQL transaction error; subsequent valid reads work.
		if row, err := q.RequireFind(t.Context(), tx, 1); err != nil || row.ID != 1 {
			t.Fatal(row, err)
		}
		return nil
	})
}
