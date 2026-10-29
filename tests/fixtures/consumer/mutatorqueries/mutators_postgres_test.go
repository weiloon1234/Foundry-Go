package mutatorqueries_test

import model "github.com/weiloon1234/Foundry-Go/model"

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
		draft := []orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, "  ADA@EXAMPLE.TEST  "), orm.InputNumberSet(mutatorqueries.Members.Attempts, 0), orm.NullableInputTextSet(mutatorqueries.Members.Nickname, ""), orm.InputEnumSet(mutatorqueries.Members.State, mutatorqueries.State(" ACTIVE "))}
		member, err := orm.Insert(t.Context(), tx, q.Table, draft...)
		if err != nil {
			return err
		}
		if member.Email != "ada@example.test" || member.Attempts != 1 || member.Nickname != value.Of("!") || member.State != mutatorqueries.Active {
			t.Fatal("create lost normalization, once-only execution, zero values or final enum validation")
		}
		inspectDone := errors.New("fresh inputs inspected")
		inputChecked := false
		inspectCtx := mutatorqueries.WithDraftInspection(t.Context(), func(w *orm.Write[mutatorqueries.Member]) error {
			email, emailOK := orm.WriteGet(w, func() func(arg0 string) orm.Assignment[mutatorqueries.Member] {
				receiver := q.Email
				return func(arg0 string) orm.Assignment[mutatorqueries.Member] { return orm.InputTextSet(receiver, arg0) }
			}()).Get()
			attempts, attemptsOK := orm.WriteGet(w, func() func(arg0 int) orm.Assignment[mutatorqueries.Member] {
				receiver := q.Attempts
				return func(arg0 int) orm.Assignment[mutatorqueries.Member] { return orm.InputNumberSet(receiver, arg0) }
			}()).Get()
			nickname, nicknameOK := orm.WriteNullableGet(w, func() func(arg0 string) orm.Assignment[mutatorqueries.Member] {
				receiver := q.Nickname
				return func(arg0 string) orm.Assignment[mutatorqueries.Member] {
					return orm.NullableInputTextSet(receiver, arg0)
				}
			}()).Get()
			name, nonnull := nickname.Get()
			state, stateOK := orm.WriteGet(w, func() func(arg0 mutatorqueries.State) orm.Assignment[mutatorqueries.Member] {
				receiver := q.State
				return func(arg0 mutatorqueries.State) orm.Assignment[mutatorqueries.Member] {
					return orm.InputEnumSet(receiver, arg0)
				}
			}()).Get()
			if !emailOK || email != "  ADA@EXAMPLE.TEST  " || !attemptsOK || attempts != 0 || !nicknameOK || !nonnull || name != "" || !stateOK || state != mutatorqueries.State(" ACTIVE ") || orm.WriteIsSet(w, func() func(arg0 model.ID[mutatorqueries.Member]) orm.Assignment[mutatorqueries.Member] {
				receiver := q.Key()
				return func(arg0 model.ID[mutatorqueries.Member]) orm.Assignment[mutatorqueries.Member] {
					return orm.KeySet(receiver, arg0)
				}
			}()) {
				return errors.New("mutator changed original inputs or omitted key")
			}
			inputChecked = true
			return inspectDone
		})
		if _, err := orm.Insert(inspectCtx, tx, q.Table, draft...); !inputChecked || !errors.Is(err, inspectDone) {
			return errors.New("fresh input inspection did not run")
		}
		// Reusing the same inputs after a rolled-back replacement must synthesize
		// a fresh key and invoke each mutator exactly once again.
		inspected := errors.New("caller inputs inspected")
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			if _, err := orm.Delete(t.Context(), child, q.Table.Query(), q.Table.Key(), member.ID); err != nil {
				return err
			}
			again, err := orm.Insert(t.Context(), child, q.Table, draft...)
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

		member, err = orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), member.ID, orm.InputTextSet(mutatorqueries.Members.Email, " NEXT@EXAMPLE.TEST "), orm.NullableInputTextSetNull(mutatorqueries.Members.Nickname))
		if err != nil {
			return err
		}
		if member.Email != "next@example.test" || !member.Nickname.IsNull() || member.Attempts != 1 {
			t.Fatal("update transformed an omitted field or replaced explicit NULL")
		}
		if _, err := orm.Update(t.Context(), tx, q.Query().Where(orm.InputTextEq(f.Email, "forbidden")), q.Key(), member.ID, orm.InputTextSet(mutatorqueries.Members.Email, "scope escape")); !errors.Is(err, database.NotFound) {
			t.Fatalf("mutator escaped query scope: %v", err)
		}
		for _, failure := range []struct {
			draft []orm.Assignment[mutatorqueries.Member]
			want  error
		}{
			{[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, "reject")}, mutatorqueries.RejectedEmail},
			{[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, "panic")}, fault.Panicked},
			{[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, "bad state"), orm.InputEnumSet(mutatorqueries.Members.State, "unknown")}, fault.Invalid},
		} {
			if got, err := orm.Insert(t.Context(), tx, q.Table, failure.draft...); !errors.Is(err, failure.want) || !got.ID.IsZero() {
				t.Fatalf("failed create published a model or lost its error: %v", err)
			}
			if got, err := orm.Update(t.Context(), tx, q.Table.Query(), q.Table.Key(), member.ID, failure.draft...); !errors.Is(err, failure.want) || !got.ID.IsZero() {
				t.Fatalf("failed update published a model or lost its error: %v", err)
			}
		}
		current, err := orm.RequireKey(t.Context(), tx, q.Table, member.ID)
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
		itemsWithStoredEmail, err := orm.All(t.Context(), tx, q.Query().Where(orm.InputTextEq(f.Email, current.Email)))
		if err != nil || len(itemsWithStoredEmail) != 1 || itemsWithStoredEmail[0] != current {
			t.Fatalf("getter evaluation changed query identity or stored hydration: %v", err)
		}
		if _, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(mutatorqueries.Members.Email, " NEXT@EXAMPLE.TEST ")); !errors.Is(err, database.UniqueViolation) {
			t.Fatalf("database uniqueness did not see the normalized value: %v", err)
		}
		defaults, err := orm.Insert(t.Context(), tx, q.Table, orm.InputTextSet(mutatorqueries.Members.Email, " DEFAULTS@EXAMPLE.TEST "))
		if err != nil {
			return err
		}
		if defaults.Attempts != 100 || defaults.Nickname != value.Of("database nickname") {
			t.Fatal("omitted fields invoked mutators on database defaults")
		}
		batch := [][]orm.Assignment[mutatorqueries.Member]{
			[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, " BATCH1@EXAMPLE.TEST "), orm.InputNumberSet(mutatorqueries.Members.Attempts, 0)},
			[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, " BATCH2@EXAMPLE.TEST "), orm.NullableInputTextSet(mutatorqueries.Members.Nickname, "two")},
		}
		items, err := orm.InsertMany(t.Context(), tx, q.Table, batch)
		if err != nil {
			return err
		}
		if len(items) != 2 || items[0].Email != "batch1@example.test" || items[0].Attempts != 1 || items[1].Nickname != value.Of("two!") {
			t.Fatal("bulk inputs did not use field mutators once")
		}
		policy := orm.OnConflict(orm.ConflictKey(orm.InputTextExpr(f.Email))).DoUpdate(orm.ConflictIncoming(orm.InputNumberExpr(f.Attempts)), orm.ConflictIncoming(orm.NullableInputTextExpr(f.Nickname)))
		upserted, err := orm.Upsert(t.Context(), tx, q.Query(), policy, orm.InputTextSet(mutatorqueries.Members.Email, " NEXT@EXAMPLE.TEST "), orm.InputNumberSet(mutatorqueries.Members.Attempts, 4), orm.NullableInputTextSet(mutatorqueries.Members.Nickname, "updated"))
		if err != nil {
			return err
		}
		upsert, present := upserted.Get()
		if !present || upsert.ID != member.ID || upsert.Attempts != 5 || upsert.Nickname != value.Of("updated!") {
			t.Fatal("conflict did not consume the normalized proposed row")
		}
		upserts, err := orm.UpsertMany(t.Context(), tx, q.Query(), policy, [][]orm.Assignment[mutatorqueries.Member]{
			[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, " BATCH1@EXAMPLE.TEST "), orm.InputNumberSet(mutatorqueries.Members.Attempts, 2), orm.NullableInputTextSetNull(mutatorqueries.Members.Nickname)},
			[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, " BATCH3@EXAMPLE.TEST "), orm.InputNumberSet(mutatorqueries.Members.Attempts, 0), orm.NullableInputTextSet(mutatorqueries.Members.Nickname, "three")},
		})
		if err != nil {
			return err
		}
		if len(upserts) != 2 || upserts[0].ID != items[0].ID || upserts[0].Attempts != 3 || !upserts[0].Nickname.IsNull() || upserts[1].Attempts != 1 || upserts[1].Nickname != value.Of("three!") {
			t.Fatal("upsert batch did not preserve normalized existing/new row inputs")
		}
		literalPolicy := orm.OnConflict(orm.ConflictKey(orm.InputTextExpr(f.Email))).DoUpdate(
			orm.ConflictSet(orm.InputTextExpr(f.Email), " CONSTANT@EXAMPLE.TEST "), orm.ConflictSet(orm.InputNumberExpr(f.Attempts), 9), orm.ConflictSet(orm.NullableInputTextExpr(f.Nickname), "constant"), orm.ConflictSet(orm.InputEnumExpr(f.State), " ACTIVE "),
		)
		for _, email := range []string{" NEXT@EXAMPLE.TEST ", " CONSTANT@EXAMPLE.TEST "} {
			result, err := orm.Upsert(t.Context(), tx, q.Query(), literalPolicy, orm.InputTextSet(mutatorqueries.Members.Email, email))
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
			{orm.ConflictSet(orm.InputTextExpr(f.Email), "reject"), mutatorqueries.RejectedEmail},
			{orm.ConflictSet(orm.InputTextExpr(f.Email), "panic"), fault.Panicked},
			{orm.ConflictSet(orm.InputEnumExpr(f.State), "unknown"), fault.Invalid},
		} {
			policy := orm.OnConflict(orm.ConflictKey(orm.InputTextExpr(f.Email))).DoUpdate(failure.update)
			if got, err := orm.Upsert(t.Context(), tx, q.Query(), policy, orm.InputTextSet(mutatorqueries.Members.Email, " CONSTANT@EXAMPLE.TEST ")); !errors.Is(err, failure.want) || got.IsSet() {
				t.Fatalf("failed conflict mutator published a model or lost its error: %v", err)
			}
		}
		proposed := rows.ConflictRowsOf(q.Query()).Proposed()
		computedPolicy := orm.OnConflict(orm.ConflictKey(orm.InputTextExpr(f.Email))).DoUpdate(rows.SetConflictValue(orm.InputNumberExpr(f.Attempts), expr.Add(expr.At(proposed, orm.InputNumberExpr(f.Attempts)), expr.Row(expr.At(proposed, orm.InputNumberExpr(f.Attempts))).Value().Param(1))))
		if _, err := orm.Upsert(t.Context(), tx, q.Query(), computedPolicy, orm.InputTextSet(mutatorqueries.Members.Email, " CONSTANT@EXAMPLE.TEST ")); !errors.Is(err, fault.Invalid) {
			t.Fatalf("SQL calculation bypassed a Go field mutator: %v", err)
		}
		unchanged, err := orm.RequireKey(t.Context(), tx, q.Table, member.ID)
		if err != nil {
			return err
		}
		if unchanged.Email != "constant@example.test" || unchanged.Attempts != 10 || unchanged.Nickname != value.Of("constant!") {
			t.Fatal("failed conflict assignment changed the stored model")
		}
		incomingPolicy := orm.OnConflict(orm.ConflictKey(orm.InputTextExpr(f.Email))).DoUpdate(rows.SetConflictValue(orm.NullableInputTextExpr(f.Nickname), expr.At(proposed, orm.NullableInputTextExpr(f.Nickname))))
		incomingResult, err := orm.Upsert(t.Context(), tx, q.Query(), incomingPolicy, orm.InputTextSet(mutatorqueries.Members.Email, " CONSTANT@EXAMPLE.TEST "), orm.NullableInputTextSet(mutatorqueries.Members.Nickname, "incoming"))
		if err != nil {
			return err
		}
		if incoming, present := incomingResult.Get(); !present || incoming.Nickname != value.Of("incoming!") {
			t.Fatal("own proposed field was rejected or transformed twice")
		}
		nullPolicy := orm.OnConflict(orm.ConflictKey(orm.InputTextExpr(f.Email))).DoUpdate(orm.ConflictNull(orm.NullableInputTextExpr(f.Nickname)))
		nullResult, err := orm.Upsert(t.Context(), tx, q.Query(), nullPolicy, orm.InputTextSet(mutatorqueries.Members.Email, " CONSTANT@EXAMPLE.TEST "))
		if err != nil {
			return err
		}
		if cleared, present := nullResult.Get(); !present || !cleared.Nickname.IsNull() {
			t.Fatal("NULL conflict literal invoked its scalar mutator")
		}
		if _, err := orm.InsertMany(t.Context(), tx, q.Table, [][]orm.Assignment[mutatorqueries.Member]{
			[]orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, "would insert")}, []orm.Assignment[mutatorqueries.Member]{orm.InputTextSet(mutatorqueries.Members.Email, "reject")},
		}); !errors.Is(err, mutatorqueries.RejectedEmail) {
			t.Fatalf("batch did not propagate its mutator veto: %v", err)
		}
		count, err := orm.Count(t.Context(), tx, q.Query())
		if err != nil {
			return err
		}
		if count != 5 {
			t.Fatalf("failed write/batch inserted data: count=%d", count)
		}
		rollback := errors.New("abort outer savepoint")
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			if _, err := orm.Insert(t.Context(), child, q.Table, orm.InputTextSet(mutatorqueries.Members.Email, "rollback")); err != nil {
				return err
			}
			return rollback
		}); !errors.Is(err, rollback) {
			return err
		}
		count, err = orm.Count(t.Context(), tx, q.Query())
		if err == nil && count != 5 {
			t.Fatal("normalized write escaped its caller's savepoint rollback")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
