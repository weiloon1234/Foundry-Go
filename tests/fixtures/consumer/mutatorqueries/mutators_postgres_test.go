package mutatorqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresAutomaticModelMutators(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{
			`SET LOCAL search_path TO "` + namespace + `"`,
			`CREATE TABLE mutator_members (id uuid PRIMARY KEY,email_address text NOT NULL UNIQUE,nickname text DEFAULT 'database nickname',attempts bigint NOT NULL DEFAULT 100,state text NOT NULL DEFAULT 'active')`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		q := mutatorqueries.QueryMutatorMembers()
		f := mutatorqueries.MemberFields()
		draft := mutatorqueries.MemberDraft{}.SetEmail("  ADA@EXAMPLE.TEST  ").SetAttempts(0).SetNickname("").SetState(mutatorqueries.State(" ACTIVE "))
		member, err := q.Create(t.Context(), tx, draft)
		if err != nil {
			return err
		}
		if member.Email != "ada@example.test" || member.Attempts != 1 || member.Nickname != value.Of("!") || member.State != mutatorqueries.Active {
			t.Fatal("create lost normalization, once-only execution, zero values or final enum validation")
		}
		if email, _ := draft.Email().Get(); email != "  ADA@EXAMPLE.TEST  " || draft.ID().IsSet() {
			t.Fatal("create changed the caller's draft")
		}
		member, err = q.Update(t.Context(), tx, member.ID, mutatorqueries.MemberDraft{}.SetEmail(" NEXT@EXAMPLE.TEST ").ClearNickname())
		if err != nil {
			return err
		}
		if member.Email != "next@example.test" || !member.Nickname.IsNull() || member.Attempts != 1 {
			t.Fatal("update transformed an omitted field or replaced explicit NULL")
		}
		if _, err := q.Where(f.Email.Eq("forbidden")).Update(t.Context(), tx, member.ID, mutatorqueries.MemberDraft{}.SetEmail("scope escape")); !errors.Is(err, database.NotFound) {
			t.Fatalf("mutator escaped query scope: %v", err)
		}
		for _, failure := range []struct {
			draft mutatorqueries.MemberDraft
			want  error
		}{
			{mutatorqueries.MemberDraft{}.SetEmail("reject"), mutatorqueries.RejectedEmail},
			{mutatorqueries.MemberDraft{}.SetEmail("panic"), fault.Panicked},
			{mutatorqueries.MemberDraft{}.SetEmail("bad state").SetState("unknown"), fault.Invalid},
		} {
			if got, err := q.Create(t.Context(), tx, failure.draft); !errors.Is(err, failure.want) || !got.ID.IsZero() {
				t.Fatalf("failed create published a model or lost its error: %v", err)
			}
			if got, err := q.Update(t.Context(), tx, member.ID, failure.draft); !errors.Is(err, failure.want) || !got.ID.IsZero() {
				t.Fatalf("failed update published a model or lost its error: %v", err)
			}
		}
		current, err := q.RequireFind(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if current != member {
			t.Fatal("failed write changed the stored model, or reading invoked a write mutator")
		}
		response, err := responseForMember(current)
		if err != nil || response.Email != "NEXT@EXAMPLE.TEST" || !response.Nickname.IsNull() || current != member {
			t.Fatalf("stored read and explicit getter DTO diverged: %+v, %v", response, err)
		}
		itemsWithStoredEmail, err := q.Where(f.Email.Eq(current.Email)).All(t.Context(), tx)
		if err != nil || len(itemsWithStoredEmail) != 1 || itemsWithStoredEmail[0] != current {
			t.Fatalf("getter evaluation changed query identity or stored hydration: %v", err)
		}
		if _, err := q.Create(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" NEXT@EXAMPLE.TEST ")); !errors.Is(err, database.UniqueViolation) {
			t.Fatalf("database uniqueness did not see the normalized value: %v", err)
		}
		defaults, err := q.Create(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" DEFAULTS@EXAMPLE.TEST "))
		if err != nil {
			return err
		}
		if defaults.Attempts != 100 || defaults.Nickname != value.Of("database nickname") {
			t.Fatal("omitted fields invoked mutators on database defaults")
		}
		batch := []mutatorqueries.MemberDraft{
			mutatorqueries.MemberDraft{}.SetEmail(" BATCH1@EXAMPLE.TEST ").SetAttempts(0),
			mutatorqueries.MemberDraft{}.SetEmail(" BATCH2@EXAMPLE.TEST ").SetNickname("two"),
		}
		items, err := q.CreateMany(t.Context(), tx, batch)
		if err != nil {
			return err
		}
		if len(items) != 2 || items[0].Email != "batch1@example.test" || items[0].Attempts != 1 || items[1].Nickname != value.Of("two!") {
			t.Fatal("bulk inputs did not use field mutators once")
		}
		policy := query.OnConflict[mutatorqueries.Member](f.Email).Update(f.Attempts, f.Nickname)
		upserted, err := q.Upsert(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" NEXT@EXAMPLE.TEST ").SetAttempts(4).SetNickname("updated"), policy)
		if err != nil {
			return err
		}
		upsert, present := upserted.Get()
		if !present || upsert.ID != member.ID || upsert.Attempts != 5 || upsert.Nickname != value.Of("updated!") {
			t.Fatal("conflict did not consume the normalized proposed row")
		}
		upserts, err := q.UpsertMany(t.Context(), tx, []mutatorqueries.MemberDraft{
			mutatorqueries.MemberDraft{}.SetEmail(" BATCH1@EXAMPLE.TEST ").SetAttempts(2).ClearNickname(),
			mutatorqueries.MemberDraft{}.SetEmail(" BATCH3@EXAMPLE.TEST ").SetAttempts(0).SetNickname("three"),
		}, policy)
		if err != nil {
			return err
		}
		if len(upserts) != 2 || upserts[0].ID != items[0].ID || upserts[0].Attempts != 3 || !upserts[0].Nickname.IsNull() || upserts[1].Attempts != 1 || upserts[1].Nickname != value.Of("three!") {
			t.Fatal("upsert batch did not preserve normalized existing/new row inputs")
		}
		literalPolicy := query.OnConflict[mutatorqueries.Member](f.Email).DoUpdate(
			f.Email.Set(" CONSTANT@EXAMPLE.TEST "), f.Attempts.Set(9), f.Nickname.Set("constant"), f.State.Set(" ACTIVE "),
		)
		for _, email := range []string{" NEXT@EXAMPLE.TEST ", " CONSTANT@EXAMPLE.TEST "} {
			result, err := q.Upsert(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(email), literalPolicy)
			if err != nil {
				return err
			}
			got, present := result.Get()
			if !present || got.ID != member.ID || got.Email != "constant@example.test" || got.Attempts != 10 || got.Nickname != value.Of("constant!") || got.State != mutatorqueries.Active {
				t.Fatal("conflict literals bypassed normalization, final validation or policy reuse")
			}
		}
		for _, failure := range []struct {
			update query.ConflictUpdate[mutatorqueries.Member]
			want   error
		}{
			{f.Email.Set("reject"), mutatorqueries.RejectedEmail},
			{f.Email.Set("panic"), fault.Panicked},
			{f.State.Set("unknown"), fault.Invalid},
		} {
			policy := query.OnConflict[mutatorqueries.Member](f.Email).DoUpdate(failure.update)
			if got, err := q.Upsert(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" CONSTANT@EXAMPLE.TEST "), policy); !errors.Is(err, failure.want) || got.IsSet() {
				t.Fatalf("failed conflict mutator published a model or lost its error: %v", err)
			}
		}
		proposed := mutatorqueries.MemberFieldsAt(q.ConflictRows().Proposed())
		computedPolicy := query.OnConflict[mutatorqueries.Member](f.Email).DoUpdate(query.SetConflictValue(f.Attempts, query.Add(proposed.Attempts, proposed.Attempts.Param(1))))
		if _, err := q.Upsert(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" CONSTANT@EXAMPLE.TEST "), computedPolicy); !errors.Is(err, fault.Invalid) {
			t.Fatalf("SQL calculation bypassed a Go field mutator: %v", err)
		}
		unchanged, err := q.RequireFind(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if unchanged.Email != "constant@example.test" || unchanged.Attempts != 10 || unchanged.Nickname != value.Of("constant!") {
			t.Fatal("failed conflict assignment changed the stored model")
		}
		incomingPolicy := query.OnConflict[mutatorqueries.Member](f.Email).DoUpdate(query.SetConflictValue(f.Nickname, proposed.Nickname))
		incomingResult, err := q.Upsert(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" CONSTANT@EXAMPLE.TEST ").SetNickname("incoming"), incomingPolicy)
		if err != nil {
			return err
		}
		if incoming, present := incomingResult.Get(); !present || incoming.Nickname != value.Of("incoming!") {
			t.Fatal("own proposed field was rejected or transformed twice")
		}
		nullPolicy := query.OnConflict[mutatorqueries.Member](f.Email).DoUpdate(f.Nickname.SetNull())
		nullResult, err := q.Upsert(t.Context(), tx, mutatorqueries.MemberDraft{}.SetEmail(" CONSTANT@EXAMPLE.TEST "), nullPolicy)
		if err != nil {
			return err
		}
		if cleared, present := nullResult.Get(); !present || !cleared.Nickname.IsNull() {
			t.Fatal("NULL conflict literal invoked its scalar mutator")
		}
		if _, err := q.CreateMany(t.Context(), tx, []mutatorqueries.MemberDraft{
			mutatorqueries.MemberDraft{}.SetEmail("would insert"), mutatorqueries.MemberDraft{}.SetEmail("reject"),
		}); !errors.Is(err, mutatorqueries.RejectedEmail) {
			t.Fatalf("batch did not propagate its mutator veto: %v", err)
		}
		count, err := q.Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 5 {
			t.Fatalf("failed write/batch inserted data: count=%d", count)
		}
		rollback := errors.New("abort outer savepoint")
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			if _, err := q.Create(t.Context(), child, mutatorqueries.MemberDraft{}.SetEmail("rollback")); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return err
		}
		count, err = q.Count(t.Context(), tx)
		if err == nil && count != 5 {
			t.Fatal("normalized write escaped its caller's savepoint rollback")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
