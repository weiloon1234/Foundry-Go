package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"foundry.test/goway/coremodels"
	newmodels "foundry.test/goway/models/mutators"
	oldmodels "foundry.test/goway/oldmodels/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

func TestMutatorsOldAndNewAgree(t *testing.T) {
	var transcripts []string
	for _, old := range []bool{true, false} {
		capture := &goldenCapture{scriptedColumns: []string{"id", "contact_email", "note", "name", "body", "enabled"}, scriptedBatches: [][][]driver.Value{
			{{int64(7), "ada@example.test", nil, "Ada", []byte{1, 2}, false}},
			{{int64(7), "ada@example.test", "note", "Ada", []byte{1, 2}, false}},
			{{int64(7), "ada@example.test", nil, "Ada", []byte{1, 2}, false}},
		}}
		db := capture.open(t.Context())
		if old {
			q := oldmodels.QueryMutatorMembers()
			member, err := q.Create(t.Context(), db, oldmodels.MemberDraft{}.SetID(7).SetEmail(coremodels.EmailInput{Address: " ADA@EXAMPLE.TEST "}).SetName(" Ada ").SetBody(coremodels.BinaryInput{1, 2}).SetEnabled(false))
			if err != nil {
				t.Fatal(err)
			}
			link, err := member.AccessEmail()
			if err != nil || link != "mailto:ada@example.test" {
				t.Fatal("old accessor", err)
			}
			_, err = q.Update(t.Context(), db, 7, oldmodels.MemberDraft{}.SetNote(coremodels.LabelInput{Text: " note "}))
			if err != nil {
				t.Fatal(err)
			}
			member, err = q.Update(t.Context(), db, 7, oldmodels.MemberDraft{}.ClearNote())
			if err != nil || !member.Note.IsNull() {
				t.Fatal("old null", err)
			}
		} else {
			q := newmodels.Members
			if err := orm.Check(q); err != nil {
				t.Fatal(err)
			}
			member, err := q.Insert(t.Context(), db, q.Key().Set(7), q.Email.Set(coremodels.EmailInput{Address: " ADA@EXAMPLE.TEST "}), q.Name.Set(" Ada "), q.Body.Set(coremodels.BinaryInput{1, 2}), q.Enabled.Set(false))
			if err != nil {
				t.Fatal(err)
			}
			link, err := member.AccessEmail()
			if err != nil || link != "mailto:ada@example.test" {
				t.Fatal("new accessor", err)
			}
			_, err = q.Update(t.Context(), db, 7, q.Note.Set(coremodels.LabelInput{Text: " note "}))
			if err != nil {
				t.Fatal(err)
			}
			member, err = q.Update(t.Context(), db, 7, q.Note.SetNull())
			if err != nil || !member.Note.IsNull() {
				t.Fatal("new null", err)
			}
		}
		transcripts = append(transcripts, capture.output.String())
		if err := db.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "mutators_golden/2", transcripts[0], transcripts[1]) {
		t.Fatalf("mutator SQL/bindings differ:\n%s", goldenDiff(transcripts[0], transcripts[1]))
	}
}

func TestInputPrimaryKeyOldAndNewAgree(t *testing.T) {
	var transcripts []string
	for _, old := range []bool{true, false} {
		capture := &goldenCapture{scriptedColumns: []string{"id", "label"}, scriptedBatches: [][][]driver.Value{
			{{orderID, "stored"}}, {{orderID, "stored"}},
		}}
		db := capture.open(t.Context())
		if old {
			id, err := model.ParseID[oldmodels.KeyRecord](orderID)
			if err != nil {
				t.Fatal(err)
			}
			q := oldmodels.QueryInputKeyRecords()
			got, err := q.Create(t.Context(), db, oldmodels.KeyRecordDraft{}.SetID(oldmodels.FreshID{Value: id}))
			if err != nil || got.ID != id {
				t.Fatal("old key input", err)
			}
			_, err = q.RequireFind(t.Context(), db, id)
			if err != nil {
				t.Fatal(err)
			}
			_, err = q.Create(t.Context(), db, oldmodels.KeyRecordDraft{})
			if !errors.Is(err, fault.Missing) {
				t.Fatal("old distinct input synthesized UUID", err)
			}
		} else {
			q := newmodels.KeyRecords
			if err := orm.Check(q); err != nil {
				t.Fatal(err)
			}
			id, err := model.ParseID[newmodels.KeyRecord](orderID)
			if err != nil {
				t.Fatal(err)
			}
			got, err := q.Insert(t.Context(), db, q.ID.Set(newmodels.FreshID{Value: id}))
			if err != nil || got.ID != id {
				t.Fatal("new key input", err)
			}
			_, err = q.Require(t.Context(), db, id)
			if err != nil {
				t.Fatal(err)
			}
			_, err = q.Insert(t.Context(), db)
			if !errors.Is(err, fault.Missing) {
				t.Fatal("new distinct input synthesized UUID", err)
			}
		}
		if capture.statements != 2 {
			t.Fatal("omitted input key reached SQL")
		}
		transcripts = append(transcripts, capture.output.String())
		if err := db.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "mutators_golden/1", transcripts[0], transcripts[1]) {
		t.Fatal(goldenDiff(transcripts[0], transcripts[1]))
	}
}
