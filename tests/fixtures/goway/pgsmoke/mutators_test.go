package pgsmoke_test

import "time"

import (
	"bytes"
	"errors"
	"testing"

	"foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func insertMutatorFixture(t *testing.T) (*database.DB, m.Member, orm.Assignment[m.Member]) {
	t.Helper()
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE mutator_members (id bigint PRIMARY KEY,contact_email text NOT NULL,note text,name text NOT NULL DEFAULT 'database',body bytea NOT NULL,enabled boolean NOT NULL DEFAULT true)`)
	must(t, err)
	q := m.Members
	must(t, orm.Check(q))
	input := coremodels.BinaryInput{1, 2}
	body := q.Body.Set(input)
	input[0] = 9
	got, err := q.Insert(ctx, db, q.Key().Set(1), q.Email.Set(coremodels.EmailInput{Address: " ADA@EXAMPLE.TEST "}), body, q.Enabled.Set(false))
	must(t, err)
	return db, got, body
}

func TestMutators(t *testing.T) {
	db, got, body := insertMutatorFixture(t)
	ctx := t.Context()
	q := m.Members
	if got.Email != "ada@example.test" || got.Name != "database" || got.Enabled || !got.Note.IsNull() || !bytes.Equal(got.Body, []byte{1, 2}) {
		t.Fatal("mutated/default/null/binary insert")
	}
	stored, err := q.Require(ctx, db, 1)
	must(t, err)
	if stored.Email != got.Email {
		t.Fatal("persisted mutated email differs")
	}
	_, err = q.Update(ctx, db, 1, q.Name.Set(" Ada "), q.Note.Set(coremodels.LabelInput{Text: " note "}))
	must(t, err)
	got, err = q.Require(ctx, db, 1)
	must(t, err)
	note, ok := got.Note.Get()
	if !ok || note != "note" || got.Name != "Ada" {
		t.Fatal("mutated update hydration")
	}
	_, err = q.Update(ctx, db, 1, q.Note.SetNull())
	must(t, err)
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := q.Update(ctx, tx, 1, q.Name.Set(" rolled back "))
		if err != nil {
			return err
		}
		_, err = q.Update(ctx, tx, 1, q.Email.Set(coremodels.EmailInput{Address: "reject"}))
		return err
	})
	if !errors.Is(err, coremodels.MutatorVeto) {
		t.Fatal("missing veto", err)
	}
	got, err = q.Require(ctx, db, 1)
	must(t, err)
	if got.Name != "Ada" || !got.Note.IsNull() {
		t.Fatal("failed transaction persisted")
	}
	_, err = q.InsertMany(ctx, db, [][]orm.Assignment[m.Member]{
		{q.Key().Set(2), q.Email.Set(coremodels.EmailInput{Address: " SECOND "}), body},
		{q.Key().Set(3), q.Email.Set(coremodels.EmailInput{Address: "reject"}), body},
	})
	if !errors.Is(err, coremodels.MutatorVeto) {
		t.Fatal("batch veto", err)
	}
	n, err := q.Query().Count(ctx, db)
	must(t, err)
	if n != 1 {
		t.Fatal("failed batch persisted")
	}
	_, err = q.Query().Where(q.Key().Eq(1)).UpdateAll(ctx, db, q.Name.Set(" set "))
	must(t, err)
	got, err = q.Require(ctx, db, 1)
	must(t, err)
	if got.Name != "set" {
		t.Fatal("set write skipped transform")
	}
}

func TestDistinctInputPrimaryKey(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `CREATE TABLE input_key_records (id uuid PRIMARY KEY,label text NOT NULL DEFAULT 'stored')`)
	must(t, err)
	q := m.KeyRecords
	must(t, orm.Check(q))
	id, err := model.NewIDAt[m.KeyRecord](time.Now())
	must(t, err)
	got, err := q.Insert(ctx, db, q.ID.Set(m.FreshID{Value: id}))
	must(t, err)
	if got.ID != id {
		t.Fatal("input primary key changed")
	}
	_, err = q.Require(ctx, db, id)
	must(t, err)
	_, err = q.Insert(ctx, db)
	if !errors.Is(err, fault.Missing) {
		t.Fatal("distinct input synthesized primary key", err)
	}
	n, err := q.Query().Count(ctx, db)
	must(t, err)
	if n != 1 {
		t.Fatal("omitted input key persisted")
	}
}
