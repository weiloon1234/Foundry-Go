package binarymodels_test

import (
	"bytes"
	"errors"
	"testing"

	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
		input := binarymodels.Input{0, 255, 1}
		draft := binarymodels.Draft(1, input)
		input[1] = 0
		created, err := binarymodels.QueryBinaryRecords().Create(t.Context(), tx, draft)
		if err != nil {
			return err
		}
		if !bytes.Equal(created.Body, []byte{0, 255, 1}) || created.Raw == nil || len(created.Raw) != 0 || !created.Note.IsNull() || created.Encoded != "00ff01" {
			return errors.New("binary persistence lost bytes, empty, NULL or custom input")
		}
		if _, err := binarymodels.QueryBinaryRecords().Create(t.Context(), tx, binarymodels.Draft(2, binarymodels.Input{}).SetNote(binarymodels.Input{})); err != nil {
			return err
		}
		fields := binarymodels.RecordFields()
		needle := binarymodels.Payload{0, 255, 1}
		selected := binarymodels.QueryBinaryRecords().Where(fields.Body.In(needle)).OrderBy(fields.Body.Asc())
		needle[0] = 7
		rows, err := selected.All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != 1 {
			return errors.New("binary membership retained caller input")
		}
		view := binarymodels.SelectView(binarymodels.QueryBinaryRecords().Where(fields.ID.Eq(2)), binarymodels.ViewSelection[binarymodels.Record]{Body: fields.Body.Value(), Note: fields.Note.Value()})
		projected, err := view.RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		note, present := projected.Note.Get()
		if projected.Body == nil || len(projected.Body) != 0 || !present || note == nil || len(note) != 0 {
			return errors.New("binary projection collapsed empty into NULL")
		}
		if n, err := binarymodels.QueryBinaryRecords().Where(fields.Note.IsNull()).Count(t.Context(), tx); err != nil || n != 1 {
			return errors.New("binary null comparison failed")
		}
		update := binarymodels.Input{4, 5}
		conflict := query.OnConflict[binarymodels.Record](fields.ID).DoUpdate(fields.Body.Set(update), fields.Note.SetNull())
		update[0] = 8
		result, err := binarymodels.QueryBinaryRecords().Upsert(t.Context(), tx, draft, conflict)
		if err != nil {
			return err
		}
		item, present := result.Get()
		if !present || !bytes.Equal(item.Body, []byte{4, 5}) || !item.Note.IsNull() {
			return errors.New("binary conflict lost owned custom input")
		}
		if _, err := binarymodels.QueryBinaryRecords().Create(t.Context(), tx, binarymodels.Draft(3, binarymodels.Input{}).SetRaw(nil)); !errors.Is(err, fault.Invalid) {
			return errors.New("nil binary model value was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
