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
		created, err := binarymodels.Records.Insert(t.Context(), tx, draft...)
		if err != nil {
			return err
		}
		if !bytes.Equal(created.Body, []byte{0, 255, 1}) || created.Raw == nil || len(created.Raw) != 0 || !created.Note.IsNull() || created.Encoded != "00ff01" {
			return errors.New("binary persistence lost bytes, empty, NULL or custom input")
		}
		if _, err := binarymodels.Records.Insert(t.Context(), tx, fields.Key().Set(2), fields.Body.Set(binarymodels.Input{}), fields.RawBytes.Set([]byte{}), fields.Note.Set(binarymodels.Input{}), fields.Encoded.Set(binarymodels.Input{})); err != nil {
			return err
		}
		needle := binarymodels.Payload{0, 255, 1}
		selected := binarymodels.Records.Query().Where(fields.Body.In(needle)).OrderBy(fields.Body.Asc())
		needle[0] = 7
		records, err := selected.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(records) != 1 || records[0].ID != 1 {
			return errors.New("binary membership retained caller input")
		}
		view := rows.Select(rows.From(binarymodels.Records.Query().Where(fields.Key().Eq(2))), binarymodels.Views.Row, rows.Map(binarymodels.Views.Body, expr.Value(fields.Body)), rows.Map(binarymodels.Views.Note, expr.Value(fields.Note)))
		projected, err := rows.Read(view).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		note, present := projected.Note.Get()
		if projected.Body == nil || len(projected.Body) != 0 || !present || note == nil || len(note) != 0 {
			return errors.New("binary projection collapsed empty into NULL")
		}
		if n, err := binarymodels.Records.Query().Where(fields.Note.IsNull()).Count(t.Context(), tx); err != nil || n != 1 {
			return errors.New("binary null comparison failed")
		}
		update := binarymodels.Input{4, 5}
		conflict := orm.OnConflict(orm.ConflictKey(fields.Key())).DoUpdate(orm.ConflictSet(fields.Body, update), orm.ConflictNull(fields.Note))
		update[0] = 8
		result, err := orm.Upsert(t.Context(), tx, binarymodels.Records.Query(), conflict, draft...)
		if err != nil {
			return err
		}
		item, present := result.Get()
		if !present || !bytes.Equal(item.Body, []byte{4, 5}) || !item.Note.IsNull() {
			return errors.New("binary conflict lost owned custom input")
		}
		if _, err := binarymodels.Records.Insert(t.Context(), tx, fields.Key().Set(3), fields.Body.Set(binarymodels.Input{}), fields.RawBytes.Set(nil), fields.Note.SetNull(), fields.Encoded.Set(binarymodels.Input{})); !errors.Is(err, fault.Invalid) {
			return errors.New("nil binary model value was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
