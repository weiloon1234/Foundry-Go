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
	body := orm.InputColumnSet(q.Body, input)
	input[0] = 9
	got, err := orm.Insert(ctx, db, q.Table, orm.KeySet(q.Key(), 1), orm.InputTextSet(q.Email, coremodels.EmailInput{Address: " ADA@EXAMPLE.TEST "}), body, orm.ColumnSet(q.Enabled, false))
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
	stored, err := orm.RequireKey(ctx, db, q.Table, 1)
	must(t, err)
	if stored.Email != got.Email {
		t.Fatal("persisted mutated email differs")
	}
	_, err = orm.Update(ctx, db, q.Table.Query(), q.Table.Key(), 1, orm.InputTextSet(q.Name, " Ada "), orm.NullableInputTextSet(q.Note, coremodels.LabelInput{Text: " note "}))
	must(t, err)
	got, err = orm.RequireKey(ctx, db, q.Table, 1)
	must(t, err)
	note, ok := got.Note.Get()
	if !ok || note != "note" || got.Name != "Ada" {
		t.Fatal("mutated update hydration")
	}
	_, err = orm.Update(ctx, db, q.Table.Query(), q.Table.Key(), 1, orm.NullableInputTextSetNull(q.Note))
	must(t, err)
	err = db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), 1, orm.InputTextSet(q.Name, " rolled back "))
		if err != nil {
			return err
		}
		_, err = orm.Update(ctx, tx, q.Table.Query(), q.Table.Key(), 1, orm.InputTextSet(q.Email, coremodels.EmailInput{Address: "reject"}))
		return err
	})
	if !errors.Is(err, coremodels.MutatorVeto) {
		t.Fatal("missing veto", err)
	}
	got, err = orm.RequireKey(ctx, db, q.Table, 1)
	must(t, err)
	if got.Name != "Ada" || !got.Note.IsNull() {
		t.Fatal("failed transaction persisted")
	}
	_, err = orm.InsertMany(ctx, db, q.Table, [][]orm.Assignment[m.Member]{
		{orm.KeySet(q.Key(), 2), orm.InputTextSet(q.Email, coremodels.EmailInput{Address: " SECOND "}), body},
		{orm.KeySet(q.Key(), 3), orm.InputTextSet(q.Email, coremodels.EmailInput{Address: "reject"}), body},
	})
	if !errors.Is(err, coremodels.MutatorVeto) {
		t.Fatal("batch veto", err)
	}
	n, err := orm.Count(ctx, db, q.Query())
	must(t, err)
	if n != 1 {
		t.Fatal("failed batch persisted")
	}
	_, err = orm.UpdateAll(ctx, db, q.Query().Where(orm.KeyEq(q.Key(), 1)), orm.InputTextSet(q.Name, " set "))
	must(t, err)
	got, err = orm.RequireKey(ctx, db, q.Table, 1)
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
	got, err := orm.Insert(ctx, db, q.Table, orm.InputKeySet(q.ID, m.FreshID{Value: id}))
	must(t, err)
	if got.ID != id {
		t.Fatal("input primary key changed")
	}
	_, err = orm.RequireKey(ctx, db, q.Table, id)
	must(t, err)
	_, err = orm.Insert(ctx, db, q.Table)
	if !errors.Is(err, fault.Missing) {
		t.Fatal("distinct input synthesized primary key", err)
	}
	n, err := orm.Count(ctx, db, q.Query())
	must(t, err)
	if n != 1 {
		t.Fatal("omitted input key persisted")
	}
}
