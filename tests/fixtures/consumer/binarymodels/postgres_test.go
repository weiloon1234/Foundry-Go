package binarymodels_test

import (
	"bytes"
	"errors"
	"testing"

	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresBinaryModelsAndProjections(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, statement := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE binary_records (id bigint PRIMARY KEY,body bytea NOT NULL,raw bytea NOT NULL,note bytea,encoded text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), statement); err != nil {
				return err
			}
		}
		fields := binarymodels.Records
		input := binarymodels.Input{0, 255, 1}
		draft := binarymodels.Draft(1, input)
		input[1] = 0
		created, err := orm.Insert(t.Context(), tx, binarymodels.Records.Table, draft...)
		if err != nil {
			return err
		}
		if !bytes.Equal(created.Body, []byte{0, 255, 1}) || created.Raw == nil || len(created.Raw) != 0 || !created.Note.IsNull() || created.Encoded != "00ff01" {
			return errors.New("binary persistence lost bytes, empty, NULL or custom input")
		}
		if _, err := orm.Insert(t.Context(), tx, binarymodels.Records.Table, orm.KeySet(fields.Key(), 2), orm.InputColumnSet(fields.Body, binarymodels.Input{}), orm.ColumnSet(fields.RawBytes, []byte{}), orm.NullableInputColumnSet(fields.Note, binarymodels.Input{}), orm.InputTextSet(fields.Encoded, binarymodels.Input{})); err != nil {
			return err
		}
		needle := binarymodels.Payload{0, 255, 1}
		selected := binarymodels.Records.Query().Where(orm.InputColumnIn(fields.Body, needle)).OrderBy(orm.InputColumnAsc(fields.Body))
		needle[0] = 7
		records, err := orm.All(t.Context(), tx, selected)
		if err != nil {
			return err
		}
		if len(records) != 1 || records[0].ID != 1 {
			return errors.New("binary membership retained caller input")
		}
		view := rows.Select(rows.From(binarymodels.Records.Query().Where(orm.KeyEq(fields.Key(), 2))), binarymodels.Views.Row, rows.Map(binarymodels.Views.Body, expr.Value(orm.InputColumnExpr(fields.Body))), rows.Map(binarymodels.Views.Note, expr.Value(orm.NullableInputColumnExpr(fields.Note))))
		projected, err := rows.Read(view).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		note, present := projected.Note.Get()
		if projected.Body == nil || len(projected.Body) != 0 || !present || note == nil || len(note) != 0 {
			return errors.New("binary projection collapsed empty into NULL")
		}
		if n, err := orm.Count(t.Context(), tx, binarymodels.Records.Query().Where(orm.NullableInputColumnIsNull(fields.Note))); err != nil || n != 1 {
			return errors.New("binary null comparison failed")
		}
		update := binarymodels.Input{4, 5}
		conflict := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(fields.Key()))).DoUpdate(orm.ConflictSet(orm.InputColumnExpr(fields.Body), update), orm.ConflictNull(orm.NullableInputColumnExpr(fields.Note)))
		update[0] = 8
		result, err := orm.Upsert(t.Context(), tx, binarymodels.Records.Query(), conflict, draft...)
		if err != nil {
			return err
		}
		item, present := result.Get()
		if !present || !bytes.Equal(item.Body, []byte{4, 5}) || !item.Note.IsNull() {
			return errors.New("binary conflict lost owned custom input")
		}
		if _, err := orm.Insert(t.Context(), tx, binarymodels.Records.Table, orm.KeySet(fields.Key(), 3), orm.InputColumnSet(fields.Body, binarymodels.Input{}), orm.ColumnSet(fields.RawBytes, nil), orm.NullableInputColumnSetNull(fields.Note), orm.InputTextSet(fields.Encoded, binarymodels.Input{})); !errors.Is(err, fault.Invalid) {
			return errors.New("nil binary model value was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
