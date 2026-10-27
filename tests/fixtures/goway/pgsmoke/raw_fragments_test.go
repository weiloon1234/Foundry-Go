package pgsmoke_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestStage6a4RawFragments(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE fragment_records (id bigint PRIMARY KEY,amount bigint NOT NULL,missing bigint,deleted_at timestamptz)`)
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO fragment_records VALUES (1,7,NULL,NULL),(2,99,NULL,now())`)
	must(t, err)
	table := orm.Define("fragment_records", func(b orm.Builder[arithmeticRecord], r *arithmeticRecord) arithmeticTable {
		return arithmeticTable{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Amount), orm.NullableNumberOf(b, &r.Missing), orm.SoftDeleteOf(b, &r.DeletedAt)}
	})
	type result struct {
		Amount int64
		Has    bool
	}
	type fields struct {
		rows.Row[result]
		Amount rows.Field[result, int64]
		Has    rows.Field[result, bool]
	}
	schema := rows.Define(func(b rows.Builder[result], r *result) fields {
		return fields{b.Row(), rows.NumberOf(b, &r.Amount), rows.FieldOf(b, &r.Has, orm.Codec(codec.Bool[bool]()))}
	})
	q := rows.Select(rows.FromModel(table.Query().Where(orm.WhereRaw[arithmeticRecord]("amount > ? OR amount = ?", 0, 0))), schema, rows.MapSelected(schema.Amount, expr.Raw[arithmeticRecord, int64]("amount + ?", 1)), rows.MapSelected(schema.Has, expr.Raw[arithmeticRecord, bool](`'{"key":1}'::jsonb ?? ?`, "key")))
	got, err := rows.Read(q).All(ctx, db)
	must(t, err)
	if len(got) != 1 || got[0].Amount != 8 || !got[0].Has {
		t.Fatal(got)
	}
	rollback := errors.New("rollback fragment test")
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE fragment_records SET amount=10 WHERE id=1")
		if e != nil {
			return e
		}
		got, e = rows.Read(q).All(ctx, tx)
		if e != nil {
			return e
		}
		if len(got) != 1 || got[0].Amount != 11 {
			t.Fatal(got)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	got, err = rows.Read(q).All(ctx, db)
	must(t, err)
	if got[0].Amount != 8 {
		t.Fatal(got)
	}
}
