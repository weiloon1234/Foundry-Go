package pgsmoke_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestTypedRawHydration(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE raw_records (id bigint PRIMARY KEY,name text NOT NULL,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO raw_records VALUES (1,'live',NULL),(2,'deleted',now())`)
	must(t, err)
	type record struct {
		ID        int64
		Name      string
		DeletedAt value.Nullable[temporal.DateTime]
	}
	table := orm.Define("raw_records", func(b orm.Builder[record], m *record) orm.Table[record, int64] {
		orm.TextOf(b, &m.Name)
		orm.NullableTimeOf(b, &m.DeletedAt)
		return orm.TableOf(b, &m.ID)
	})
	got, err := table.Raw(ctx, db, `SELECT deleted_at,name AS name,id FROM raw_records ORDER BY id`)
	must(t, err)
	if len(got) != 2 || got[1].DeletedAt.IsNull() {
		t.Fatal("raw applied soft-delete scope", got)
	}
	ordinary, err := table.Query().All(ctx, db)
	must(t, err)
	if len(ordinary) != 1 || ordinary[0].ID != 1 {
		t.Fatal("ordinary soft-delete scope", ordinary)
	}
	type summary struct {
		ID    int64
		Label string
	}
	schema := rows.Define(func(b rows.Builder[summary], m *summary) rows.Row[summary] {
		rows.NumberOf(b, &m.ID)
		rows.TextOf(b, &m.Label)
		return b.Row()
	})
	projected, err := rows.Raw(ctx, db, schema, `SELECT name AS label,id FROM raw_records WHERE id=$1`, int64(2))
	must(t, err)
	if len(projected) != 1 || projected[0].Label != "deleted" {
		t.Fatal(projected)
	}
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		updated, e := table.Raw(ctx, tx, `UPDATE raw_records SET name=$1 WHERE id=$2 RETURNING name,deleted_at,id`, "changed", int64(1))
		if e == nil && (len(updated) != 1 || updated[0].Name != "changed") {
			t.Fatal(updated)
		}
		return e
	})
	must(t, err)
	persisted, err := table.Require(ctx, db, 1)
	must(t, err)
	if persisted.Name != "changed" {
		t.Fatal("raw write not persisted")
	}
	if got, err := table.Raw(ctx, db, `SELECT id FROM raw_records WHERE false`); !errors.Is(err, fault.Invalid) || got != nil {
		t.Fatal("missing column accepted", got, err)
	}
}
