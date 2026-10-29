package mutatorqueries_test

import (
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresStoredFieldChangesAfterMutators(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE mutator_members (id uuid PRIMARY KEY,email_address text NOT NULL,nickname text,attempts bigint NOT NULL DEFAULT 100,state text NOT NULL DEFAULT 'active')`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		q := mutatorqueries.Members
		createDraft := []orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(q.Email, " ADA@EXAMPLE.TEST ")}
		before, err := orm.Insert(t.Context(), tx, q.Table, createDraft...)
		if err != nil {
			return err
		}
		patch := []orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(q.Email, "  ADA@example.test  "), orm.NullableInputTextSet(q.Nickname, "")}
		after, err := orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), before.ID, patch...)
		if err != nil {
			return err
		}
		// Comparison uses stored snapshots and the declaration codecs.
		changes, err := orm.Compare(q.Table, value.Set(before), value.Set(after), patch...)
		if err != nil {
			return err
		}
		email, err := orm.ChangeOf(changes, orm.InputTextExpr(q.Email))
		if err != nil {
			return err
		}
		if !email.Assigned() || email.Changed() {
			t.Fatal("normalized assignment should not change stored email")
		}
		nickname, err := orm.NullableChangeOf(changes, orm.NullableInputTextExpr(q.Nickname))
		if err != nil {
			return err
		}
		if !nickname.Assigned() || !nickname.Changed() {
			t.Fatal("NULL to normalized empty input was not a change")
		}
		if stored, present := nickname.After().Get(); !present || stored != value.Of("!") {
			t.Fatal("change comparison ran the nickname mutator again or lost its value")
		}
		// Create supplied an omitted UUID. The comparison boundary accepts the
		// effective assignment draft; the hook pipeline will reconstruct it.
		created, err := orm.Compare(q.Table, value.Optional[mutatorqueries.Member]{}, value.Set(before), append(createDraft, orm.KeySet(q.Key(), before.ID))...)
		if err != nil {
			return err
		}
		createdDefault, err := orm.ChangeOf(created, orm.InputNumberExpr(q.Attempts))
		if err != nil {
			return err
		}
		createdID, err := orm.ChangeOf(created, orm.KeyExpr(q.Key()))
		if err != nil {
			return err
		}
		if createdDefault.Assigned() || !createdDefault.Changed() || !createdID.Assigned() {
			t.Fatal("database-owned default or generated UUID lost creation state")
		}
		removed, err := orm.Delete(t.Context(), tx, q.Table.Query(), q.Table.Key(), after.ID)
		if err != nil {
			return err
		}
		deleted, err := orm.Compare(q.Table, value.Set(removed), value.Optional[mutatorqueries.Member]{})
		deletedID, fieldErr := orm.ChangeOf(deleted, orm.KeyExpr(q.Key()))
		if err != nil || fieldErr != nil || !deleted.Changed() || deleted.Assigned() || deleted.After().IsSet() || !deletedID.Changed() {
			t.Fatalf("deletion lost persisted snapshot or existence change: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
