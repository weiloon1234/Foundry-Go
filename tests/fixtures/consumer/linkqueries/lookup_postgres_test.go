package linkqueries_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresLookupWritesChooseOneLifecycleBranch(t *testing.T) {
	trace := &linkqueries.Trace{}
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		q := linkqueries.Memberships
		fields := linkqueries.Memberships
		draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code), orm.InputTextSet(linkqueries.Memberships.Role, " ADMIN ")}
		selected := q.Query().Where(orm.InputTextEq(fields.Role, "admin"))
		created, err := orm.FirstOrCreate(ctx, wrappedTransactor{tx}, selected, draft...)
		if err != nil {
			return err
		}
		if created.Role != "admin" || len(trace.Steps) != 8 || trace.Reads != 0 {
			return errors.New("lookup creation bypassed normal lifecycle or dispatched internal retrieval")
		}
		trace.Steps = nil
		// This draft cannot create a valid membership; a found branch must leave it unused.
		found, err := orm.FirstOrCreate(ctx, tx, selected)
		if err != nil {
			return err
		}
		if found.ID != created.ID || len(trace.Steps) != 0 || trace.Reads != 0 {
			return errors.New("found lookup prepared creation or ran model callbacks")
		}
		calls := 0
		update := func(ctx context.Context, owner *database.Tx, current linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
			calls++
			if current.ID != created.ID {
				return []orm.Assignment[linkqueries.Membership]{}, errors.New("lookup callback received wrong stored model")
			}
			if count, err := orm.Count(ctx, owner, q.Query()); err != nil || count != 1 {
				return []orm.Assignment[linkqueries.Membership]{}, errors.New("lookup callback lost its usable transaction")
			}
			return []orm.Assignment[linkqueries.Membership]{orm.InputTextSet(linkqueries.Memberships.Role, " EDITOR ")}, nil
		}
		updated, err := orm.UpdateOrCreate(ctx, wrappedTransactor{tx}, selected, []orm.Assignment[linkqueries.Membership]{}, update)
		if err != nil {
			return err
		}
		wantUpdateSteps := []string{"local.saving", "provider.saving", "local.saved", "provider.saved"}
		if updated.ID != created.ID || updated.Role != "editor" || calls != 1 || !reflect.DeepEqual(trace.Steps, wantUpdateSteps) {
			return errors.New("lookup update chose wrong branch or lost mutators")
		}
		// An ordinary update may move its result outside the original filter.
		if count, err := orm.Count(t.Context(), tx, selected); err != nil || count != 0 {
			return errors.New("updated model did not leave its original scope")
		}
		trace.Steps = nil
		missing := q.Query().Where(orm.InputTextEq(fields.Role, "new"))
		next, err := orm.UpdateOrCreate(ctx, tx, missing, []orm.Assignment[linkqueries.Membership]{draft[0], draft[1], orm.InputTextSet(linkqueries.Memberships.Role, " NEW ")}, func(context.Context, *database.Tx, linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
			calls++
			return []orm.Assignment[linkqueries.Membership]{}, errors.New("missing branch invoked update callback")
		})
		if err != nil {
			return err
		}
		if next.Role != "new" || calls != 1 || len(trace.Steps) != 8 || len(trace.Committed) != 0 {
			return errors.New("missing update-or-create branch lost lifecycle or outer commit ownership")
		}
		return nil
	})
	want := []lifecycle.Operation{lifecycle.Create, lifecycle.Create, lifecycle.Update, lifecycle.Update, lifecycle.Create, lifecycle.Create}
	if !reflect.DeepEqual(trace.Committed, want) {
		t.Fatal("lookup operations lost normal after-commit order", trace.Committed)
	}
}

func TestPostgresLookupScopesAndFailuresRollback(t *testing.T) {
	for _, mode := range []string{"creation_scope", "callback", "after_hook"} {
		t.Run(mode, func(t *testing.T) {
			trace := &linkqueries.Trace{}
			runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				member, group, err := ormEndpoints(t, tx)
				if err != nil {
					return err
				}
				q := linkqueries.Memberships
				fields := linkqueries.Memberships
				draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code), orm.InputTextSet(linkqueries.Memberships.Role, "admin")}
				ctx := linkqueries.WithTrace(t.Context(), trace)
				if mode == "creation_scope" {
					_, err := orm.FirstOrCreate(ctx, tx, q.Query().Where(orm.InputTextEq(fields.Role, "different")), draft...)
					if !errors.Is(err, fault.Invalid) {
						return errors.New("creation escaped its lookup predicates")
					}
					if count, err := orm.Count(t.Context(), tx, q.Query()); err != nil || count != 0 {
						return errors.New("failed lookup postcondition retained an insert")
					}
					return nil
				}
				current, err := orm.Insert(t.Context(), tx, q.Table, draft...)
				if err != nil {
					return err
				}
				if mode == "after_hook" {
					trace.VetoAt = "local.saved"
				}
				_, err = orm.UpdateOrCreate(ctx, tx, q.Query().Where(orm.KeyEq(fields.Key(), current.ID)), []orm.Assignment[linkqueries.Membership]{}, func(context.Context, *database.Tx, linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
					if mode == "callback" {
						return []orm.Assignment[linkqueries.Membership]{}, linkqueries.ErrVeto
					}
					return []orm.Assignment[linkqueries.Membership]{orm.InputTextSet(linkqueries.Memberships.Role, "changed")}, nil
				})
				if !errors.Is(err, linkqueries.ErrVeto) {
					return errors.New("lookup write lost callback or hook failure")
				}
				stored, err := orm.RequireKey(t.Context(), tx, q.Table, current.ID)
				if err != nil {
					return err
				}
				if stored.Role != "admin" {
					return errors.New("failed lookup write retained a change")
				}
				return nil
			})
			if len(trace.Committed) != 0 {
				t.Fatal("failed lookup write retained after-commit callbacks")
			}
		})
	}
}

func TestPostgresLookupPrimaryOrderAndSoftVisibility(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		q := linkqueries.Memberships
		fields := linkqueries.Memberships
		draft := []orm.Assignment[linkqueries.Membership]{orm.ColumnSet(linkqueries.Memberships.MemberID, member.ID), orm.TextSet(linkqueries.Memberships.GroupCode, group.Code), orm.InputTextSet(linkqueries.Memberships.Role, "same")}
		if _, err := orm.InsertMany(t.Context(), tx, q.Table, [][]orm.Assignment[linkqueries.Membership]{draft, draft}); err != nil {
			return err
		}
		first, err := orm.Require(t.Context(), tx, q.Query())
		if err != nil {
			return err
		}
		found, err := orm.FirstOrCreate(t.Context(), tx, q.Query())
		if err != nil {
			return err
		}
		if found.ID != first.ID {
			return errors.New("lookup did not use ordinary first primary ordering")
		}
		if _, err := orm.Delete(t.Context(), tx, q.Table.Query(), q.Table.Key(), first.ID); err != nil {
			return err
		}
		found, err = orm.FirstOrCreate(t.Context(), tx, q.Query().OnlyTrashed())
		if err != nil {
			return err
		}
		if found.ID != first.ID || found.DeletedAt.IsNull() {
			return errors.New("lookup ignored explicit trashed visibility")
		}
		replacement, err := orm.FirstOrCreate(t.Context(), tx, q.Query().Where(orm.KeyEq(fields.Key(), first.ID)), append(slices.Clone(draft), orm.KeySet(linkqueries.Memberships.Key(), first.ID))...)
		if !errors.Is(err, database.UniqueViolation) || replacement.ID != (linkqueries.Membership{}).ID {
			return errors.New("hidden deleted key did not retain ordinary uniqueness failure")
		}
		if count, err := orm.Count(t.Context(), tx, q.Query().WithTrashed()); err != nil || count != 2 {
			return errors.New("lookup conflict damaged the parent transaction")
		}
		// Natural identities stay concrete and an existing row does not need its creation key again.
		natural, err := orm.FirstOrCreate(t.Context(), tx, linkqueries.Groups.Query().Where(orm.KeyEq(linkqueries.Groups.Key(), group.Code)))
		if err != nil {
			return err
		}
		if natural.Code != group.Code {
			return errors.New("natural lookup identity changed")
		}
		return nil
	})
}
