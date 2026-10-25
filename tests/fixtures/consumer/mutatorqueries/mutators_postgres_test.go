package mutatorqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
		q := mutatorqueries.Members
		f := mutatorqueries.Members
		draft := []orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set("  ADA@EXAMPLE.TEST  "), mutatorqueries.Members.Attempts.Set(0), mutatorqueries.Members.Nickname.Set(""), mutatorqueries.Members.State.Set(mutatorqueries.State(" ACTIVE "))}
		member, err := q.Insert(t.Context(), tx, draft...)
		if err != nil {
			return err
		}
		if member.Email != "ada@example.test" || member.Attempts != 1 || member.Nickname != value.Of("!") || member.State != mutatorqueries.Active {
			t.Fatal("create lost normalization, once-only execution, zero values or final enum validation")
		}
		inspectDone := errors.New("fresh inputs inspected")
		inputChecked := false
		inspectCtx := mutatorqueries.WithDraftInspection(t.Context(), func(w *orm.Write[mutatorqueries.Member]) error {
			email, emailOK := orm.WriteGet(w, q.Email.Set).Get()
			attempts, attemptsOK := orm.WriteGet(w, q.Attempts.Set).Get()
			nickname, nicknameOK := orm.WriteNullableGet(w, q.Nickname.Set).Get()
			name, nonnull := nickname.Get()
			state, stateOK := orm.WriteGet(w, q.State.Set).Get()
			if !emailOK || email != "  ADA@EXAMPLE.TEST  " || !attemptsOK || attempts != 0 || !nicknameOK || !nonnull || name != "" || !stateOK || state != mutatorqueries.State(" ACTIVE ") || orm.WriteIsSet(w, q.Key().Set) {
				return errors.New("mutator changed original inputs or omitted key")
			}
			inputChecked = true
			return inspectDone
		})
		if _, err := q.Insert(inspectCtx, tx, draft...); !inputChecked || !errors.Is(err, inspectDone) {
			return errors.New("fresh input inspection did not run")
		}
		// Reusing the same inputs after a rolled-back replacement must synthesize
		// a fresh key and invoke each mutator exactly once again.
		inspected := errors.New("caller inputs inspected")
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			if _, err := q.Delete(t.Context(), child, member.ID); err != nil {
				return err
			}
			again, err := q.Insert(t.Context(), child, draft...)
			if err != nil {
				return err
			}
			if again.ID == member.ID || again.Email != "ada@example.test" || again.Attempts != 1 || again.Nickname != value.Of("!") {
				return errors.New("write preparation mutated caller inputs")
			}
			return inspected
		}); !errors.Is(err, inspected) {
			return err
		}

		member, err = q.Update(t.Context(), tx, member.ID, mutatorqueries.Members.Email.Set(" NEXT@EXAMPLE.TEST "), mutatorqueries.Members.Nickname.SetNull())
		if err != nil {
			return err
		}
		if member.Email != "next@example.test" || !member.Nickname.IsNull() || member.Attempts != 1 {
			t.Fatal("update transformed an omitted field or replaced explicit NULL")
		}
		if _, err := orm.Update(t.Context(), tx, q.Query().Where(f.Email.Eq("forbidden")), q.Key(), member.ID, mutatorqueries.Members.Email.Set("scope escape")); !errors.Is(err, database.NotFound) {
			t.Fatalf("mutator escaped query scope: %v", err)
		}
		for _, failure := range []struct {
			draft []orm.Assignment[mutatorqueries.Member]
			want  error
		}{
			{[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set("reject")}, mutatorqueries.RejectedEmail},
			{[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set("panic")}, fault.Panicked},
			{[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set("bad state"), mutatorqueries.Members.State.Set("unknown")}, fault.Invalid},
		} {
			if got, err := q.Insert(t.Context(), tx, failure.draft...); !errors.Is(err, failure.want) || !got.ID.IsZero() {
				t.Fatalf("failed create published a model or lost its error: %v", err)
			}
			if got, err := q.Update(t.Context(), tx, member.ID, failure.draft...); !errors.Is(err, failure.want) || !got.ID.IsZero() {
				t.Fatalf("failed update published a model or lost its error: %v", err)
			}
		}
		current, err := q.Require(t.Context(), tx, member.ID)
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
		itemsWithStoredEmail, err := q.Query().Where(f.Email.Eq(current.Email)).All(t.Context(), tx)
		if err != nil || len(itemsWithStoredEmail) != 1 || itemsWithStoredEmail[0] != current {
			t.Fatalf("getter evaluation changed query identity or stored hydration: %v", err)
		}
		if _, err := q.Insert(t.Context(), tx, mutatorqueries.Members.Email.Set(" NEXT@EXAMPLE.TEST ")); !errors.Is(err, database.UniqueViolation) {
			t.Fatalf("database uniqueness did not see the normalized value: %v", err)
		}
		defaults, err := q.Insert(t.Context(), tx, mutatorqueries.Members.Email.Set(" DEFAULTS@EXAMPLE.TEST "))
		if err != nil {
			return err
		}
		if defaults.Attempts != 100 || defaults.Nickname != value.Of("database nickname") {
			t.Fatal("omitted fields invoked mutators on database defaults")
		}
		batch := [][]orm.Assignment[mutatorqueries.Member]{
			[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set(" BATCH1@EXAMPLE.TEST "), mutatorqueries.Members.Attempts.Set(0)},
			[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set(" BATCH2@EXAMPLE.TEST "), mutatorqueries.Members.Nickname.Set("two")},
		}
		items, err := q.InsertMany(t.Context(), tx, batch)
		if err != nil {
			return err
		}
		if len(items) != 2 || items[0].Email != "batch1@example.test" || items[0].Attempts != 1 || items[1].Nickname != value.Of("two!") {
			t.Fatal("bulk inputs did not use field mutators once")
		}
		policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictIncoming(f.Attempts), orm.ConflictIncoming(f.Nickname))
		upserted, err := orm.Upsert(t.Context(), tx, q.Query(), policy, mutatorqueries.Members.Email.Set(" NEXT@EXAMPLE.TEST "), mutatorqueries.Members.Attempts.Set(4), mutatorqueries.Members.Nickname.Set("updated"))
		if err != nil {
			return err
		}
		upsert, present := upserted.Get()
		if !present || upsert.ID != member.ID || upsert.Attempts != 5 || upsert.Nickname != value.Of("updated!") {
			t.Fatal("conflict did not consume the normalized proposed row")
		}
		upserts, err := orm.UpsertMany(t.Context(), tx, q.Query(), policy, [][]orm.Assignment[mutatorqueries.Member]{
			[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set(" BATCH1@EXAMPLE.TEST "), mutatorqueries.Members.Attempts.Set(2), mutatorqueries.Members.Nickname.SetNull()},
			[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set(" BATCH3@EXAMPLE.TEST "), mutatorqueries.Members.Attempts.Set(0), mutatorqueries.Members.Nickname.Set("three")},
		})
		if err != nil {
			return err
		}
		if len(upserts) != 2 || upserts[0].ID != items[0].ID || upserts[0].Attempts != 3 || !upserts[0].Nickname.IsNull() || upserts[1].Attempts != 1 || upserts[1].Nickname != value.Of("three!") {
			t.Fatal("upsert batch did not preserve normalized existing/new row inputs")
		}
		literalPolicy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(
			orm.ConflictSet(f.Email, " CONSTANT@EXAMPLE.TEST "), orm.ConflictSet(f.Attempts, 9), orm.ConflictSet(f.Nickname, "constant"), orm.ConflictSet(f.State, " ACTIVE "),
		)
		for _, email := range []string{" NEXT@EXAMPLE.TEST ", " CONSTANT@EXAMPLE.TEST "} {
			result, err := orm.Upsert(t.Context(), tx, q.Query(), literalPolicy, mutatorqueries.Members.Email.Set(email))
			if err != nil {
				return err
			}
			got, present := result.Get()
			if !present || got.ID != member.ID || got.Email != "constant@example.test" || got.Attempts != 10 || got.Nickname != value.Of("constant!") || got.State != mutatorqueries.Active {
				t.Fatal("conflict literals bypassed normalization, final validation or policy reuse")
			}
		}
		for _, failure := range []struct {
			update orm.ConflictUpdate[mutatorqueries.Member]
			want   error
		}{
			{orm.ConflictSet(f.Email, "reject"), mutatorqueries.RejectedEmail},
			{orm.ConflictSet(f.Email, "panic"), fault.Panicked},
			{orm.ConflictSet(f.State, "unknown"), fault.Invalid},
		} {
			policy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(failure.update)
			if got, err := orm.Upsert(t.Context(), tx, q.Query(), policy, mutatorqueries.Members.Email.Set(" CONSTANT@EXAMPLE.TEST ")); !errors.Is(err, failure.want) || got.IsSet() {
				t.Fatalf("failed conflict mutator published a model or lost its error: %v", err)
			}
		}
		proposed := rows.ConflictRowsOf(q.Query()).Proposed()
		computedPolicy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(rows.SetConflictValue(f.Attempts, expr.Add(expr.At(proposed, f.Attempts), expr.Select(expr.At(proposed, f.Attempts)).Param(1))))
		if _, err := orm.Upsert(t.Context(), tx, q.Query(), computedPolicy, mutatorqueries.Members.Email.Set(" CONSTANT@EXAMPLE.TEST ")); !errors.Is(err, fault.Invalid) {
			t.Fatalf("SQL calculation bypassed a Go field mutator: %v", err)
		}
		unchanged, err := q.Require(t.Context(), tx, member.ID)
		if err != nil {
			return err
		}
		if unchanged.Email != "constant@example.test" || unchanged.Attempts != 10 || unchanged.Nickname != value.Of("constant!") {
			t.Fatal("failed conflict assignment changed the stored model")
		}
		incomingPolicy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(rows.SetConflictValue(f.Nickname, expr.At(proposed, f.Nickname)))
		incomingResult, err := orm.Upsert(t.Context(), tx, q.Query(), incomingPolicy, mutatorqueries.Members.Email.Set(" CONSTANT@EXAMPLE.TEST "), mutatorqueries.Members.Nickname.Set("incoming"))
		if err != nil {
			return err
		}
		if incoming, present := incomingResult.Get(); !present || incoming.Nickname != value.Of("incoming!") {
			t.Fatal("own proposed field was rejected or transformed twice")
		}
		nullPolicy := orm.OnConflict(orm.ConflictKey(f.Email)).DoUpdate(orm.ConflictNull(f.Nickname))
		nullResult, err := orm.Upsert(t.Context(), tx, q.Query(), nullPolicy, mutatorqueries.Members.Email.Set(" CONSTANT@EXAMPLE.TEST "))
		if err != nil {
			return err
		}
		if cleared, present := nullResult.Get(); !present || !cleared.Nickname.IsNull() {
			t.Fatal("NULL conflict literal invoked its scalar mutator")
		}
		if _, err := q.InsertMany(t.Context(), tx, [][]orm.Assignment[mutatorqueries.Member]{
			[]orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set("would insert")}, []orm.Assignment[mutatorqueries.Member]{mutatorqueries.Members.Email.Set("reject")},
		}); !errors.Is(err, mutatorqueries.RejectedEmail) {
			t.Fatalf("batch did not propagate its mutator veto: %v", err)
		}
		count, err := q.Query().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		if count != 5 {
			t.Fatalf("failed write/batch inserted data: count=%d", count)
		}
		rollback := errors.New("abort outer savepoint")
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			if _, err := q.Insert(t.Context(), child, mutatorqueries.Members.Email.Set("rollback")); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return err
		}
		count, err = q.Query().Count(t.Context(), tx)
		if err == nil && count != 5 {
			t.Fatal("normalized write escaped its caller's savepoint rollback")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
