package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"foundry.test/goway/coremodels"
	newmodels "foundry.test/goway/models/mutators"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

func TestMutatorsOldAndNewAgree(t *testing.T) {
	var transcripts []string
	{
		capture := &goldenCapture{scriptedColumns: []string{"id", "contact_email", "note", "name", "body", "enabled"}, scriptedBatches: [][][]driver.Value{
			{{int64(7), "ada@example.test", nil, "Ada", []byte{1, 2}, false}},
			{{int64(7), "ada@example.test", "note", "Ada", []byte{1, 2}, false}},
			{{int64(7), "ada@example.test", nil, "Ada", []byte{1, 2}, false}},
		}}
		db := capture.open(t.Context())

		q := newmodels.Members
		if err := orm.Check(q); err != nil {
			t.Fatal(err)
		}
		member, err := orm.Insert(t.Context(), db, q.Table, orm.KeySet(q.Key(), 7), orm.InputTextSet(q.Email, coremodels.EmailInput{Address: " ADA@EXAMPLE.TEST "}), orm.InputTextSet(q.Name, " Ada "), orm.InputColumnSet(q.Body, coremodels.BinaryInput{1, 2}), orm.ColumnSet(q.Enabled, false))
		if err != nil {
			t.Fatal(err)
		}
		link, err := member.AccessEmail()
		if err != nil || link != "mailto:ada@example.test" {
			t.Fatal("new accessor", err)
		}
		_, err = orm.Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 7, orm.NullableInputTextSet(q.Note, coremodels.LabelInput{Text: " note "}))
		if err != nil {
			t.Fatal(err)
		}
		member, err = orm.Update(t.Context(), db, q.Table.Query(), q.Table.Key(), 7, orm.NullableInputTextSetNull(q.Note))
		if err != nil || !member.Note.IsNull() {
			t.Fatal("new null", err)
		}

		transcripts = append(transcripts, capture.output.String())
		if err := db.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "mutators_golden/2", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}

func TestInputPrimaryKeyOldAndNewAgree(t *testing.T) {
	var transcripts []string
	{
		capture := &goldenCapture{scriptedColumns: []string{"id", "label"}, scriptedBatches: [][][]driver.Value{
			{{orderID, "stored"}}, {{orderID, "stored"}},
		}}
		db := capture.open(t.Context())

		q := newmodels.KeyRecords
		if err := orm.Check(q); err != nil {
			t.Fatal(err)
		}
		id, err := model.ParseID[newmodels.KeyRecord](orderID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := orm.Insert(t.Context(), db, q.Table, orm.InputKeySet(q.ID, newmodels.FreshID{Value: id}))
		if err != nil || got.ID != id {
			t.Fatal("new key input", err)
		}
		_, err = orm.RequireKey(t.Context(), db, q.Table, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = orm.Insert(t.Context(), db, q.Table)
		if !errors.Is(err, fault.Missing) {
			t.Fatal("new distinct input synthesized UUID", err)
		}

		if capture.statements != 2 {
			t.Fatal("omitted input key reached SQL")
		}
		transcripts = append(transcripts, capture.output.String())
		if err := db.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "mutators_golden/1", transcripts[0]) {
		t.Fatal("frozen legacy comparison differs")
	}
}
