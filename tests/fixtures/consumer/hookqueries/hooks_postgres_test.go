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
	"github.com/weiloon1234/Foundry-Go/database/query"
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
	_, err := hookqueries.QueryHookMembers().CreateMany(t.Context(), tx, []hookqueries.MemberDraft{
		hookqueries.MemberDraft{}.SetEmail(" INTRODUCER@EXAMPLE.TEST ").SetIsIntroducer(true),
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
		q := hookqueries.QueryHookMembers()
		input := hookqueries.MemberDraft{}.SetNickname("")
		member, err := q.Create(hookqueries.WithTrace(t.Context(), &create), tx, input)
		if err != nil {
			return err
		}
		if member.Email != "default@example.test" || member.Nickname != value.Of("!") || member.IntroducerID.IsNull() || member.Rank != 100 {
			t.Fatal("hook default, mutator or database default was lost")
		}
		if input.Email().IsSet() || input.ID().IsSet() || input.IntroducerID().IsSet() {
			t.Fatal("hook modified caller's immutable draft")
		}
		if !reflect.DeepEqual(create.Calls, []string{"saving", "creating", "created", "saved"}) {
			t.Fatalf("create order: %v", create.Calls)
		}
		changes := create.Changes[0]
		if changes.Before().IsSet() || !changes.After().IsSet() || !changes.Fields().ID.Assigned() || !changes.Fields().IntroducerID.Assigned() || changes.Fields().Rank.Assigned() || !changes.Fields().Rank.Changed() {
			t.Fatal("automatic capture lost UUID, hook assignment or omitted default")
		}
		member, err = q.Update(hookqueries.WithTrace(t.Context(), &update), tx, member.ID, hookqueries.MemberDraft{}.SetEmail(" DEFAULT@EXAMPLE.TEST ").ClearNickname())
		if err != nil {
			return err
		}
		changes = update.Changes[0]
		if !changes.Before().IsSet() || !changes.After().IsSet() || !changes.Fields().Email.Assigned() || changes.Fields().Email.Changed() || !changes.Fields().Nickname.Changed() || !member.Nickname.IsNull() {
			t.Fatal("update capture lost assigned-versus-changed or NULL")
		}
		if !reflect.DeepEqual(update.Calls, []string{"saving", "updating", "updated", "saved"}) {
			t.Fatalf("update order: %v", update.Calls)
		}
		var empty hookqueries.Trace
		member, err = q.Update(hookqueries.WithTrace(t.Context(), &empty), tx, member.ID, hookqueries.MemberDraft{})
		if err != nil {
			return err
		}
		if member.Rank != 101 || !empty.Changes[0].Fields().Rank.Assigned() {
			t.Fatal("before hook could not supply an empty patch")
		}
		deleted, err := q.Delete(hookqueries.WithTrace(t.Context(), &remove), tx, member.ID)
		if err != nil {
			return err
		}
		if deleted != member || !reflect.DeepEqual(remove.Calls, []string{"deleting", "deleted"}) {
			t.Fatal("delete hook order or result changed")
		}
		changes = remove.Changes[0]
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
		q, logs := hookqueries.QueryHookMembers(), hookqueries.QueryHookLogs()
		member, err := q.Create(t.Context(), tx, hookqueries.MemberDraft{}.SetEmail("existing"))
		if err != nil {
			return err
		}
		baseline, err := logs.Count(t.Context(), tx)
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
					got, err = q.Update(ctx, tx, member.ID, hookqueries.MemberDraft{}.SetEmail("changed"))
				case "deleting", "deleted":
					got, err = q.Delete(ctx, tx, member.ID)
				default:
					got, err = q.Create(ctx, tx, hookqueries.MemberDraft{}.SetEmail("new"))
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
				if count, err := logs.Count(t.Context(), tx); err != nil || count != baseline {
					t.Fatalf("hook writes escaped rollback: %d %v", count, err)
				}
				if stored, err := q.RequireFind(t.Context(), tx, member.ID); err != nil || stored != member {
					t.Fatalf("failed hooks modified model: %v", err)
				}
			}
		}
		if err := tx.Transaction(t.Context(), func(child *database.Tx) error {
			_, err := q.Create(hookqueries.WithTrace(t.Context(), &rolledBack), child, hookqueries.MemberDraft{}.SetEmail("rollback"))
			if err != nil {
				return err
			}
			return hookqueries.Veto
		}); !errors.Is(err, hookqueries.Veto) {
			t.Fatalf("nested rollback: %v", err)
		}
		var bulk hookqueries.Trace
		ctx := hookqueries.WithTrace(t.Context(), &bulk)
		if _, err := q.CreateMany(ctx, tx, []hookqueries.MemberDraft{hookqueries.MemberDraft{}.SetEmail(" BULK ")}); err != nil {
			return err
		}
		if _, err := q.Upsert(ctx, tx, hookqueries.MemberDraft{}.SetEmail(" BULK "), query.OnConflict[hookqueries.Member](hookqueries.MemberFields().Email).DoNothing()); err != nil {
			return err
		}
		if _, err := q.UpsertMany(ctx, tx, []hookqueries.MemberDraft{hookqueries.MemberDraft{}.SetEmail(" BULK ")}, query.OnConflict[hookqueries.Member](hookqueries.MemberFields().Email).DoNothing()); err != nil {
			return err
		}
		if len(bulk.Calls) != 0 {
			t.Fatal("set-based writes ran per-model hooks")
		}
		var invalid hookqueries.Trace
		if _, err := q.Update(hookqueries.WithTrace(t.Context(), &invalid), tx, member.ID, hookqueries.MemberDraft{}.SetID(member.ID)); !errors.Is(err, fault.Invalid) || len(invalid.Calls) != 0 {
			t.Fatalf("invalid primary mutation ran hooks: %v", err)
		}
		if _, err := q.Where(hookqueries.MemberFields().Email.Eq("missing")).Update(hookqueries.WithTrace(t.Context(), &invalid), tx, member.ID, hookqueries.MemberDraft{}.SetEmail("change")); !errors.Is(err, database.NotFound) || len(invalid.Calls) != 0 {
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
		q := hookqueries.QueryHookMembers()
		for _, stage := range []string{"saving", "creating", "created", "saved"} {
			ctx, cancel := context.WithCancel(t.Context())
			trace := hookqueries.Trace{FailAt: stage, Failure: "cancel", Cancel: cancel}
			_, err := q.Create(hookqueries.WithTrace(ctx, &trace), tx, hookqueries.MemberDraft{})
			cancel()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s cancellation: %v", stage, err)
			}
		}
		trace := hookqueries.Trace{Recurse: true}
		var diagnostic *fault.Error
		if _, err := q.Create(hookqueries.WithTrace(t.Context(), &trace), tx, hookqueries.MemberDraft{}); !errors.Is(err, fault.Invalid) || !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message(), "nesting") {
			t.Fatalf("recursive hooks lacked bounded diagnostics: %v", err)
		}
		if count, err := hookqueries.QueryHookLogs().Count(t.Context(), tx); err != nil || count != 0 {
			t.Fatalf("cancellation/recursion leaked hook rows: %d %v", count, err)
		}
		_, err := q.Create(hookqueries.WithTrace(t.Context(), &commit), tx, hookqueries.MemberDraft{})
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
		count, err := hookqueries.QueryHookMembers().Count(t.Context(), tx)
		if err == nil && count != 2 {
			t.Fatal("committed hook error rolled back persisted data")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
