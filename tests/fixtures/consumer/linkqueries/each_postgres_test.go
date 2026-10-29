package linkqueries_test

import temporal "github.com/weiloon1234/Foundry-Go/temporal"

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresPerModelBatchLifecycle(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, clock *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		q := linkqueries.Memberships.Query()
		fields := linkqueries.Memberships
		draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code)}
		inputs := [][]orm.Assignment[linkqueries.Membership]{append(slices.Clone(draft), orm.InputTextSet(linkqueries.Memberships.Role, " ADMIN ")), append(slices.Clone(draft), orm.InputTextSet(linkqueries.Memberships.Role, " VIEWER "))}
		trace.OnPhase = func(_ context.Context, phase string) error {
			if phase == "local.creating" {
				clock.Advance(time.Second)
			}
			return nil
		}
		created, err := orm.CreateEach(ctx, wrappedTransactor{tx}, q, inputs)
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
		for i, input := range inputs {
			inspected := false
			inspectDone := errors.New("batch input inspected")
			trace.InspectMembership = func(w *orm.Write[linkqueries.Membership]) error {
				role, ok := orm.WriteGet(w, func() func(arg0 string) orm.Assignment[linkqueries.Membership] {
					receiver := fields.Role
					return func(arg0 string) orm.Assignment[linkqueries.Membership] { return orm.InputTextSet(receiver, arg0) }
				}()).Get()
				if !ok || role != []string{" ADMIN ", " VIEWER "}[i] || orm.WriteIsSet(w, func() func(arg0 model.ID[linkqueries.Membership]) orm.Assignment[linkqueries.Membership] {
					receiver := fields.Key()
					return func(arg0 model.ID[linkqueries.Membership]) orm.Assignment[linkqueries.Membership] {
						return orm.KeySet(receiver, arg0)
					}
				}()) || orm.WriteIsSet(w, func() func(arg0 time.Time) orm.Assignment[linkqueries.Membership] {
					receiver := fields.CreatedAt
					return func(arg0 time.Time) orm.Assignment[linkqueries.Membership] { return orm.TimeSet(receiver, arg0) }
				}()) || orm.WriteIsSet(w, func() func(arg0 temporal.DateTime) orm.Assignment[linkqueries.Membership] {
					receiver := fields.UpdatedAt
					return func(arg0 temporal.DateTime) orm.Assignment[linkqueries.Membership] {
						return orm.TimeSet(receiver, arg0)
					}
				}()) {
					return errors.New("batch creation changed its input draft")
				}
				inspected = true
				return inspectDone
			}
			_, err := orm.Insert(ctx, tx, linkqueries.Memberships.Table, input...)
			trace.InspectMembership = nil
			if !inspected || !errors.Is(err, inspectDone) {
				return errors.New("batch input inspection failed")
			}
		}
		ordered, err := orm.All(t.Context(), tx, q.OrderBy(orm.KeyAsc(fields.Key())))
		if err != nil {
			return err
		}
		var expected, visited []model.ID[linkqueries.Membership]
		for _, row := range ordered {
			expected = append(expected, row.ID)
		}
		callback := func(ctx context.Context, owner *database.Tx, current linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
			visited = append(visited, current.ID)
			// A callback can perform typed I/O on the same connection after
			// candidate rows close, retaining the actual transaction owner.
			if count, err := orm.Count(ctx, owner, q); err != nil || count != 2 {
				return []orm.Assignment[linkqueries.Membership]{}, errors.New("callback lost its usable transaction")
			}
			return []orm.Assignment[linkqueries.Membership]{orm.InputTextSet(linkqueries.Memberships.Role, " OWNER-"+current.Role+" ")}, nil
		}
		trace.Steps = nil
		if rows, err := orm.UpdateEach(ctx, tx, q, 1, callback); !errors.Is(err, fault.Invalid) || rows != nil {
			return errors.New("oversized update did not fail atomically")
		}
		if len(visited) != 0 || len(trace.Steps) != 0 {
			return errors.New("oversized set invoked callbacks or hooks")
		}
		updated, err := orm.UpdateEach(ctx, wrappedTransactor{tx}, q, 2, callback)
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
		if rows, err := orm.DeleteEach(ctx, tx, q, 1); !errors.Is(err, fault.Invalid) || rows != nil || len(trace.Steps) != 0 {
			return errors.New("oversized deletion ran a pivot hook")
		}
		deleted, err := orm.DeleteEach(ctx, tx, q, 2)
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
		none, err := orm.DeleteEach(ctx, tx, q.OnlyTrashed(), 2)
		if err != nil || len(none) != 0 {
			return errors.New("ordinary per-model deletion rewrote trashed rows")
		}
		restored, err := orm.RestoreEach(ctx, tx, q, linkqueries.Memberships.Soft, 2)
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
		removed, err := orm.ForceDeleteEach(ctx, tx, q, linkqueries.Memberships.Soft, 2)
		if err != nil {
			return err
		}
		if len(removed) != 2 {
			return errors.New("per-model force deletion lost rows")
		}
		if count, err := orm.Count(t.Context(), tx, q.WithTrashed()); err != nil || count != 0 {
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
			runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				member, group, err := ormEndpoints(t, tx)
				if err != nil {
					return err
				}
				q := linkqueries.Memberships.Query()
				draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code), orm.InputTextSet(linkqueries.Memberships.Role, "admin")}
				if _, err := orm.CreateEach(t.Context(), tx, q, [][]orm.Assignment[linkqueries.Membership]{draft, draft}); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx = linkqueries.WithTrace(ctx, trace)
				calls := 0
				changed, err := orm.UpdateEach(ctx, tx, q, 2, func(ctx context.Context, owner *database.Tx, current linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
					calls++
					if err := owner.AfterCommit(func(context.Context) error { callbackCommits++; return nil }); err != nil {
						return []orm.Assignment[linkqueries.Membership]{}, err
					}
					if calls == 2 {
						switch mode {
						case "error":
							return []orm.Assignment[linkqueries.Membership]{}, linkqueries.ErrVeto
						case "panic":
							panic("private batch callback payload")
						case "goexit":
							runtime.Goexit()
						case "cancel":
							cancel()
						}
					}
					return []orm.Assignment[linkqueries.Membership]{orm.InputTextSet(linkqueries.Memberships.Role, "changed")}, nil
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
				rows, err := orm.All(t.Context(), tx, q)
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
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		q := linkqueries.Memberships.Query()
		draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code), orm.InputTextSet(linkqueries.Memberships.Role, "admin")}
		rows, err := orm.CreateEach(linkqueries.WithTrace(t.Context(), trace), tx, q, [][]orm.Assignment[linkqueries.Membership]{draft, draft})
		if !errors.Is(err, linkqueries.ErrVeto) || rows != nil || createdCallbacks != 2 {
			return errors.New("later post-insert hook did not fail the creation batch")
		}
		if count, err := orm.Count(t.Context(), tx, q); err != nil || count != 0 {
			return errors.New("failed creation batch retained an earlier insert")
		}
		return nil
	})
	if len(trace.Committed) != 0 {
		t.Fatal("failed creation batch retained after-commit callbacks")
	}
}

func TestPostgresEachSoftFailuresRollBackStoredModels(t *testing.T) {
	for _, operation := range []string{"restore", "force-delete"} {
		for _, mode := range []string{"bound", "before", "after", "cancel"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				trace := &linkqueries.Trace{}
				runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
					member, group, err := ormEndpoints(t, tx)
					if err != nil {
						return err
					}
					fields := linkqueries.Memberships
					draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(fields.MemberID, member.ID), orm.TextSet(fields.GroupCode, group.Code), orm.InputTextSet(fields.Role, "member")}
					if _, err := orm.CreateEach(t.Context(), tx, fields.Query(), [][]orm.Assignment[linkqueries.Membership]{draft, draft}); err != nil {
						return err
					}
					if _, err := orm.DeleteEach(t.Context(), tx, fields.Query(), 2); err != nil {
						return err
					}
					before, err := orm.All(t.Context(), tx, fields.Query().OnlyTrashed().OrderBy(orm.KeyAsc(fields.Key())))
					if err != nil {
						return err
					}
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					ctx = linkqueries.WithTrace(ctx, trace)
					phase := "local.restoring"
					if operation == "force-delete" {
						phase = "local.force-deleting"
					}
					if mode == "after" {
						phase = "local.restored"
						if operation == "force-delete" {
							phase = "local.force-deleted"
						}
					}
					calls := 0
					trace.OnPhase = func(_ context.Context, current string) error {
						if current == phase {
							calls++
							if calls == 2 {
								if mode == "cancel" {
									cancel()
									return nil
								}
								return linkqueries.ErrVeto
							}
						}
						return nil
					}
					limit := 2
					if mode == "bound" {
						limit = 1
					}
					var changed []linkqueries.Membership
					if operation == "restore" {
						changed, err = orm.RestoreEach(ctx, tx, fields.Query(), fields.Soft, limit)
					} else {
						changed, err = orm.ForceDeleteEach(ctx, tx, fields.Query().OnlyTrashed(), fields.Soft, limit)
					}
					want := linkqueries.ErrVeto
					if mode == "bound" {
						want = fault.Invalid
					}
					if mode == "cancel" {
						want = context.Canceled
					}
					if !errors.Is(err, want) || changed != nil {
						return errors.New("soft batch failure published models or lost its classification")
					}
					if mode == "bound" && len(trace.Steps) != 0 {
						return errors.New("oversized soft batch invoked hooks")
					}
					after, err := orm.All(t.Context(), tx, fields.Query().WithTrashed().OrderBy(orm.KeyAsc(fields.Key())))
					if err != nil {
						return err
					}
					if !reflect.DeepEqual(before, after) {
						return errors.New("failed soft batch retained an earlier model change")
					}
					return nil
				})
				if len(trace.Committed) != 0 {
					t.Fatal("failed soft batch published after-commit work")
				}
			})
		}
	}
}
