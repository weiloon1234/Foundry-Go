package mutatorqueries_test

import (
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
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
		q := mutatorqueries.QueryMutatorMembers()
		createDraft := mutatorqueries.MemberDraft{}.SetEmail(" ADA@EXAMPLE.TEST ")
		before, err := q.Create(t.Context(), tx, createDraft)
		if err != nil {
			return err
		}
		patch := mutatorqueries.MemberDraft{}.SetEmail("  ADA@example.test  ").SetNickname("")
		after, err := q.Update(t.Context(), tx, before.ID, patch)
		if err != nil {
			return err
		}
		// The generated comparison owns every field and codec. The next hook
		// increment will own automatic snapshot/assignment capture as well.
		changes, err := mutatorqueries.CompareMember(value.Set(before), value.Set(after), patch)
		if err != nil {
			return err
		}
		email := changes.Fields().Email
		if !email.Assigned() || email.Changed() {
			t.Fatal("normalized assignment should not change stored email")
		}
		nickname := changes.Fields().Nickname
		if !nickname.Assigned() || !nickname.Changed() {
			t.Fatal("NULL to normalized empty input was not a change")
		}
		if stored, present := nickname.After().Get(); !present || stored != value.Of("!") {
			t.Fatal("change comparison ran the nickname mutator again or lost its value")
		}
		// Create supplied an omitted UUID. The comparison boundary accepts the
		// effective assignment draft; the hook pipeline will reconstruct it.
		created, err := mutatorqueries.CompareMember(value.Optional[mutatorqueries.Member]{}, value.Set(before), createDraft.SetID(before.ID))
		if err != nil {
			return err
		}
		createdDefault := created.Fields().Attempts
		if createdDefault.Assigned() || !createdDefault.Changed() || !created.Fields().ID.Assigned() {
			t.Fatal("database-owned default or generated UUID lost creation state")
		}
		removed, err := q.Delete(t.Context(), tx, after.ID)
		if err != nil {
			return err
		}
		deleted, err := mutatorqueries.CompareMember(value.Set(removed), value.Optional[mutatorqueries.Member]{}, mutatorqueries.MemberDraft{})
		if err != nil || !deleted.Changed() || deleted.Assigned() || deleted.After().IsSet() || !deleted.Fields().ID.Changed() {
			t.Fatalf("deletion lost persisted snapshot or existence change: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
