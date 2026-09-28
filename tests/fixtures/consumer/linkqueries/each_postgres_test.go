package linkqueries_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresPerModelBatchLifecycle(t *testing.T) {
	trace := &linkqueries.Trace{}
	runLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		q := linkqueries.QueryLinkMemberships()
		fields := linkqueries.MembershipFields()
		draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code)
		inputs := []linkqueries.MembershipDraft{draft.SetRole(" ADMIN "), draft.SetRole(" VIEWER ")}
		trace.OnPhase = func(_ context.Context, phase string) error {
			if phase == "local.creating" {
				clock.Advance(time.Second)
			}
			return nil
		}
		created, err := q.CreateEach(ctx, wrappedTransactor{tx}, inputs)
		if err != nil {
			return err
		}
		if len(created) != 2 || created[0].Role != "admin" || created[1].Role != "viewer" || created[0].ID == created[1].ID {
			return errors.New("per-model creation lost input order, mutators or identities")
		}
		trace.OnPhase = nil
		if created[1].CreatedAt.Sub(created[0].CreatedAt) != time.Second {
			return errors.New("creation batch did not sample time in each normal lifecycle")
		}
		for _, input := range inputs {
			if input.ID().IsSet() {
				return errors.New("batch creation changed its input draft")
			}
		}
		ordered, err := q.OrderBy(fields.ID.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		var expected, visited []model.ID[linkqueries.Membership]
		for _, row := range ordered {
			expected = append(expected, row.ID)
		}
		callback := func(ctx context.Context, owner *database.Tx, current linkqueries.Membership) (linkqueries.MembershipDraft, error) {
			visited = append(visited, current.ID)
			// A callback can perform typed I/O on the same connection after
			// candidate rows close, retaining the actual transaction owner.
			if count, err := q.Count(ctx, owner); err != nil || count != 2 {
				return linkqueries.MembershipDraft{}, errors.New("callback lost its usable transaction")
			}
			return linkqueries.MembershipDraft{}.SetRole(" OWNER-" + current.Role + " "), nil
		}
		trace.Steps = nil
		if rows, err := q.UpdateEach(ctx, tx, 1, callback); !errors.Is(err, fault.Invalid) || rows != nil {
			return errors.New("oversized update did not fail atomically")
		}
		if len(visited) != 0 || len(trace.Steps) != 0 {
			return errors.New("oversized set invoked callbacks or hooks")
		}
		updated, err := q.UpdateEach(ctx, wrappedTransactor{tx}, 2, callback)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(visited, expected) || len(updated) != 2 {
			return errors.New("per-model update did not follow its locked primary order")
		}
		for _, row := range updated {
			if !strings.HasPrefix(row.Role, "owner-") {
				return errors.New("per-model update bypassed field mutators")
			}
		}
		trace.Steps = nil
		if rows, err := q.DeleteEach(ctx, tx, 1); !errors.Is(err, fault.Invalid) || rows != nil || len(trace.Steps) != 0 {
			return errors.New("oversized deletion ran a pivot hook")
		}
		deleted, err := q.DeleteEach(ctx, tx, 2)
		if err != nil {
			return err
		}
		if len(deleted) != 2 {
			return errors.New("per-model deletion lost a row")
		}
		for _, row := range deleted {
			if row.DeletedAt.IsNull() {
				return errors.New("per-model deletion bypassed soft deletion")
			}
		}
		none, err := q.OnlyTrashed().DeleteEach(ctx, tx, 2)
		if err != nil || len(none) != 0 {
			return errors.New("ordinary per-model deletion rewrote trashed rows")
		}
		restored, err := q.RestoreEach(ctx, tx, 2)
		if err != nil {
			return err
		}
		if len(restored) != 2 {
			return errors.New("per-model restoration lost rows")
		}
		for _, row := range restored {
			if !row.DeletedAt.IsNull() {
				return errors.New("per-model restoration retained its timestamp")
			}
		}
		removed, err := q.ForceDeleteEach(ctx, tx, 2)
		if err != nil {
			return err
		}
		if len(removed) != 2 {
			return errors.New("per-model force deletion lost rows")
		}
		if count, err := q.WithTrashed().Count(t.Context(), tx); err != nil || count != 0 {
			return errors.New("per-model force deletion retained rows")
		}
		if len(trace.Committed) != 0 {
			return errors.New("per-model callbacks ran before the outer commit")
		}
		return nil
	})
	var want []lifecycle.Operation
	for _, operation := range []lifecycle.Operation{lifecycle.Create, lifecycle.Update, lifecycle.SoftDelete, lifecycle.Restore, lifecycle.ForceDelete} {
		for range 4 {
			want = append(want, operation)
		}
	}
	if !reflect.DeepEqual(trace.Committed, want) {
		t.Fatal("batch lost normal model operations or outer-commit callbacks", trace.Committed)
	}
}

func TestPostgresPerModelCallbackFailuresAreAtomic(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			trace := &linkqueries.Trace{}
			callbackCommits := 0
			runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				member, group, err := endpoints(t, tx)
				if err != nil {
					return err
				}
				q := linkqueries.QueryLinkMemberships()
				draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole("admin")
				if _, err := q.CreateEach(t.Context(), tx, []linkqueries.MembershipDraft{draft, draft}); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx = linkqueries.WithTrace(ctx, trace)
				calls := 0
				changed, err := q.UpdateEach(ctx, tx, 2, func(ctx context.Context, owner *database.Tx, current linkqueries.Membership) (linkqueries.MembershipDraft, error) {
					calls++
					if err := owner.AfterCommit(func(context.Context) error { callbackCommits++; return nil }); err != nil {
						return linkqueries.MembershipDraft{}, err
					}
					if calls == 2 {
						switch mode {
						case "error":
							return linkqueries.MembershipDraft{}, linkqueries.ErrVeto
						case "panic":
							panic("private batch callback payload")
						case "goexit":
							runtime.Goexit()
						case "cancel":
							cancel()
						}
					}
					return linkqueries.MembershipDraft{}.SetRole("changed"), nil
				})
				want := linkqueries.ErrVeto
				if mode == "panic" || mode == "goexit" {
					want = fault.Panicked
				}
				if mode == "cancel" {
					want = context.Canceled
				}
				if !errors.Is(err, want) || changed != nil || calls != 2 {
					return errors.New("per-model callback failure lost its kind or published a result")
				}
				if strings.Contains(err.Error(), "private batch callback payload") {
					return errors.New("per-model callback panic exposed its payload")
				}
				rows, err := q.All(t.Context(), tx)
				if err != nil {
					return err
				}
				if len(rows) != 2 {
					return errors.New("failed batch lost stored models")
				}
				for _, row := range rows {
					if row.Role != "admin" {
						return errors.New("later callback failure retained an earlier model update")
					}
				}
				return nil
			})
			if callbackCommits != 0 || len(trace.Committed) != 0 {
				t.Fatal("failed batch retained after-commit work")
			}
		})
	}
}

func TestPostgresPerModelCreationAfterHookFailureRollsBack(t *testing.T) {
	trace := &linkqueries.Trace{}
	createdCallbacks := 0
	trace.OnPhase = func(_ context.Context, phase string) error {
		if phase == "local.created" {
			createdCallbacks++
			if createdCallbacks == 2 {
				return linkqueries.ErrVeto
			}
		}
		return nil
	}
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		q := linkqueries.QueryLinkMemberships()
		draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole("admin")
		rows, err := q.CreateEach(linkqueries.WithTrace(t.Context(), trace), tx, []linkqueries.MembershipDraft{draft, draft})
		if !errors.Is(err, linkqueries.ErrVeto) || rows != nil || createdCallbacks != 2 {
			return errors.New("later post-insert hook did not fail the creation batch")
		}
		if count, err := q.Count(t.Context(), tx); err != nil || count != 0 {
			return errors.New("failed creation batch retained an earlier insert")
		}
		return nil
	})
	if len(trace.Committed) != 0 {
		t.Fatal("failed creation batch retained after-commit callbacks")
	}
}
