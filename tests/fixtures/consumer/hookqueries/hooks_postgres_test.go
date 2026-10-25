package hookqueries_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/hookqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func prepare(t *testing.T, tx *database.Tx, namespace string) error {
	t.Helper()
	for _, statement := range []string{
		`SET LOCAL search_path TO "` + namespace + `"`,
		`CREATE TABLE hook_members(id uuid PRIMARY KEY,email text NOT NULL UNIQUE,nickname text,introducer_id uuid REFERENCES hook_members(id),is_introducer boolean NOT NULL DEFAULT false,rank bigint NOT NULL DEFAULT 100)`,
		`CREATE TABLE hook_logs(id uuid PRIMARY KEY,label text NOT NULL)`,
	} {
		if _, err := tx.Exec(t.Context(), statement); err != nil {
			return err
		}
	}
	_, err := hookqueries.Members.InsertMany(t.Context(), tx, [][]orm.Assignment[hookqueries.Member]{
		{hookqueries.Members.Email.Set(" INTRODUCER@EXAMPLE.TEST "), hookqueries.Members.IsIntroducer.Set(true)},
	})
	return err
}

func TestPostgresAutomaticModelHooks(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	var create, update, remove hookqueries.Trace
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := prepare(t, tx, namespace); err != nil {
			return err
		}
		q := hookqueries.Members
		input := []orm.Assignment[hookqueries.Member]{hookqueries.Members.Nickname.Set("")}
		member, err := q.Insert(hookqueries.WithTrace(t.Context(), &create), tx, input...)
		if err != nil {
			return err
		}
		if member.Email != "default@example.test" || member.Nickname != value.Of("!") || member.IntroducerID.IsNull() || member.Rank != 100 {
			t.Fatal("hook default, mutator or database default was lost")
		}
		inspectDone := errors.New("original draft inspected")
		inspected := false
		inspection := hookqueries.Trace{Inspect: func(w *orm.Write[hookqueries.Member]) error {
			nickname, set := orm.WriteNullableGet(w, q.Nickname.Set).Get()
			name, nonnull := nickname.Get()
			if !set || !nonnull || name != "" || orm.WriteIsSet(w, q.Email.Set) || orm.WriteIsSet(w, q.Key().Set) || orm.WriteIsSet(w, q.IntroducerID.Set) {
				return errors.New("hook modified caller values or omissions")
			}
			inspected = true
			return inspectDone
		}}
		_, inspectErr := q.Insert(hookqueries.WithTrace(t.Context(), &inspection), tx, input...)
		if !inspected || !errors.Is(inspectErr, inspectDone) {
			return errors.New("original draft inspection failed")
		}

		if !reflect.DeepEqual(create.Calls, []string{"saving", "creating", "created", "saved"}) {
			t.Fatalf("create order: %v", create.Calls)
		}
		changes := create.ORMChanges[0]
		if changes.Before().IsSet() || !changes.After().IsSet() || !checkedChange(orm.ChangeOf(changes, hookqueries.Members.Key())).Assigned() || !checkedChange(orm.NullableChangeOf(changes, hookqueries.Members.IntroducerID)).Assigned() || checkedChange(orm.ChangeOf(changes, hookqueries.Members.Rank)).Assigned() || !checkedChange(orm.ChangeOf(changes, hookqueries.Members.Rank)).Changed() {
			t.Fatal("automatic capture lost UUID, hook assignment or omitted default")
		}
		member, err = q.Update(hookqueries.WithTrace(t.Context(), &update), tx, member.ID, hookqueries.Members.Email.Set(" DEFAULT@EXAMPLE.TEST "), hookqueries.Members.Nickname.SetNull())
		if err != nil {
			return err
		}
		changes = update.ORMChanges[0]
		if !changes.Before().IsSet() || !changes.After().IsSet() || !checkedChange(orm.ChangeOf(changes, hookqueries.Members.Email)).Assigned() || checkedChange(orm.ChangeOf(changes, hookqueries.Members.Email)).Changed() || !checkedChange(orm.NullableChangeOf(changes, hookqueries.Members.Nickname)).Changed() || !member.Nickname.IsNull() {
			t.Fatal("update capture lost assigned-versus-changed or NULL")
		}
		if !reflect.DeepEqual(update.Calls, []string{"saving", "updating", "updated", "saved"}) {
			t.Fatalf("update order: %v", update.Calls)
		}
		var empty hookqueries.Trace
		member, err = q.Update(hookqueries.WithTrace(t.Context(), &empty), tx, member.ID)
		if err != nil {
			return err
		}
		if member.Rank != 101 || !checkedChange(orm.ChangeOf(empty.ORMChanges[0], hookqueries.Members.Rank)).Assigned() {
			t.Fatal("before hook could not supply an empty patch")
		}
		deleted, err := q.Delete(hookqueries.WithTrace(t.Context(), &remove), tx, member.ID)
		if err != nil {
			return err
		}
		if deleted != member || !reflect.DeepEqual(remove.Calls, []string{"deleting", "deleted"}) {
			t.Fatal("delete hook order or result changed")
		}
		changes = remove.ORMChanges[0]
		if !changes.Before().IsSet() || changes.After().IsSet() || changes.Assigned() {
			t.Fatal("deletion capture lost absence or assignment state")
		}
		var skipped hookqueries.Trace
		if _, err := q.Delete(hookqueries.WithTrace(t.Context(), &skipped), tx, member.ID); !errors.Is(err, database.NotFound) || len(skipped.Calls) != 0 {
			t.Fatalf("missing row ran hooks: %v", err)
		}
		if len(create.Operations)+len(update.Operations)+len(remove.Operations) != 0 {
			t.Fatal("savepoint release ran after-commit hooks")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		trace     *hookqueries.Trace
		operation lifecycle.Operation
	}{{&create, lifecycle.Create}, {&update, lifecycle.Update}, {&remove, lifecycle.Delete}} {
		if !reflect.DeepEqual(item.trace.Operations, []lifecycle.Operation{item.operation}) || item.trace.Calls[len(item.trace.Calls)-1] != "committed" {
			t.Fatal("outer commit did not publish each callback once")
		}
	}
}

func TestPostgresHookRollbackAndBulkSemantics(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	var rolledBack hookqueries.Trace
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := prepare(t, tx, namespace); err != nil {
			return err
		}
		q, logs := hookqueries.Members, hookqueries.Logs
		member, err := q.Insert(t.Context(), tx, hookqueries.Members.Email.Set("existing"))
		if err != nil {
			return err
		}
		baseline, err := logs.Query().Count(t.Context(), tx)
		if err != nil {
			return err
		}
		for _, name := range []string{"saving", "creating", "created", "saved", "updating", "updated", "deleting", "deleted"} {
			for _, mode := range []string{"error", "panic", "goexit"} {
				trace := hookqueries.Trace{FailAt: name, Failure: mode}
				ctx := hookqueries.WithTrace(t.Context(), &trace)
				var got hookqueries.Member
				var err error
				switch name {
				case "updating", "updated":
					got, err = q.Update(ctx, tx, member.ID, hookqueries.Members.Email.Set("changed"))
				case "deleting", "deleted":
					got, err = q.Delete(ctx, tx, member.ID)
				default:
					got, err = q.Insert(ctx, tx, hookqueries.Members.Email.Set("new"))
				}
				want := hookqueries.Veto
				if mode == "panic" || mode == "goexit" {
					want = fault.Panicked
				}
				if !errors.Is(err, want) || !got.ID.IsZero() {
					t.Fatalf("%s/%s lost failure or published a result: %v", name, mode, err)
				}
				if strings.Contains(err.Error(), "private hook panic") {
					t.Fatal("panic payload exposed")
				}
				if count, err := logs.Query().Count(t.Context(), tx); err != nil || count != baseline {
					t.Fatalf("hook writes escaped rollback: %d %v", count, err)
				}
				if stored, err := q.Require(t.Context(), tx, member.ID); err != nil || stored != member {
					t.Fatalf("failed hooks modified model: %v", err)
				}
			}
		}
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			_, err := q.Insert(hookqueries.WithTrace(t.Context(), &rolledBack), child, hookqueries.Members.Email.Set("rollback"))
			if err != nil {
				return err
			}
			return hookqueries.Veto
		}); !errors.Is(err, hookqueries.Veto) {
			t.Fatalf("nested rollback: %v", err)
		}
		var bulk hookqueries.Trace
		ctx := hookqueries.WithTrace(t.Context(), &bulk)
		if _, err := q.InsertMany(ctx, tx, [][]orm.Assignment[hookqueries.Member]{{hookqueries.Members.Email.Set(" BULK ")}}); err != nil {
			return err
		}
		if _, err := orm.Upsert(ctx, tx, q.Query(), orm.OnConflict(orm.ConflictKey(hookqueries.Members.Email)).DoNothing(), hookqueries.Members.Email.Set(" BULK ")); err != nil {
			return err
		}
		if _, err := orm.UpsertMany(ctx, tx, q.Query(), orm.OnConflict(orm.ConflictKey(hookqueries.Members.Email)).DoNothing(), [][]orm.Assignment[hookqueries.Member]{{hookqueries.Members.Email.Set(" BULK ")}}); err != nil {
			return err
		}
		if len(bulk.Calls) != 0 {
			t.Fatal("set-based writes ran per-model hooks")
		}
		var invalid hookqueries.Trace
		if _, err := q.Update(hookqueries.WithTrace(t.Context(), &invalid), tx, member.ID, hookqueries.Members.Key().Set(member.ID)); !errors.Is(err, fault.Invalid) || len(invalid.Calls) != 0 {
			t.Fatalf("invalid primary mutation ran hooks: %v", err)
		}
		if _, err := orm.Update(hookqueries.WithTrace(t.Context(), &invalid), tx, q.Query().Where(q.Email.Eq("missing")), q.Key(), member.ID, hookqueries.Members.Email.Set("change")); !errors.Is(err, database.NotFound) || len(invalid.Calls) != 0 {
			t.Fatalf("scope mismatch ran hooks: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rolledBack.Operations) != 0 {
		t.Fatal("rolled-back savepoint retained after-commit callbacks")
	}
}

func TestPostgresHookCancellationRecursionAndCommitFailure(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	commit := hookqueries.Trace{FailAt: "committed"}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if err := prepare(t, tx, namespace); err != nil {
			return err
		}
		q := hookqueries.Members
		for _, stage := range []string{"saving", "creating", "created", "saved"} {
			ctx, cancel := context.WithCancel(t.Context())
			trace := hookqueries.Trace{FailAt: stage, Failure: "cancel", Cancel: cancel}
			_, err := q.Insert(hookqueries.WithTrace(ctx, &trace), tx)
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s cancellation: %v", stage, err)
			}
		}
		trace := hookqueries.Trace{Recurse: true}
		var diagnostic *fault.Error
		if _, err := q.Insert(hookqueries.WithTrace(t.Context(), &trace), tx); !errors.Is(err, fault.Invalid) || !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message(), "nesting") {
			t.Fatalf("recursive hooks lacked bounded diagnostics: %v", err)
		}
		if count, err := hookqueries.Logs.Query().Count(t.Context(), tx); err != nil || count != 0 {
			t.Fatalf("cancellation/recursion leaked hook rows: %d %v", count, err)
		}
		_, err := q.Insert(hookqueries.WithTrace(t.Context(), &commit), tx)
		return err
	})
	var outcome *database.Error
	if !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, hookqueries.Veto) || !errors.As(err, &outcome) || outcome.Outcome() != database.Committed {
		t.Fatalf("after-commit failure lost committed outcome: %v", err)
	}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+namespace+`"`); err != nil {
			return err
		}
		count, err := hookqueries.Members.Query().Count(t.Context(), tx)
		if err == nil && count != 2 {
			t.Fatal("committed hook error rolled back persisted data")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkedChange[V any](field lifecycle.FieldChange[V], err error) lifecycle.FieldChange[V] {
	if err != nil {
		panic(err)
	}
	return field
}
