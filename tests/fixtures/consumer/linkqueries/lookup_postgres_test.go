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
		draft := []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.MemberID.Set(member.ID), linkqueries.Memberships.GroupCode.Set(group.Code), linkqueries.Memberships.Role.Set(" ADMIN ")}
		selected := q.Query().Where(fields.Role.Eq("admin"))
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
			if count, err := q.Query().Count(ctx, owner); err != nil || count != 1 {
				return []orm.Assignment[linkqueries.Membership]{}, errors.New("lookup callback lost its usable transaction")
			}
			return []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.Role.Set(" EDITOR ")}, nil
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
		if count, err := selected.Count(t.Context(), tx); err != nil || count != 0 {
			return errors.New("updated model did not leave its original scope")
		}
		trace.Steps = nil
		missing := q.Query().Where(fields.Role.Eq("new"))
		next, err := orm.UpdateOrCreate(ctx, tx, missing, []orm.Assignment[linkqueries.Membership]{draft[0], draft[1], linkqueries.Memberships.Role.Set(" NEW ")}, func(context.Context, *database.Tx, linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
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
				draft := []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.MemberID.Set(member.ID), linkqueries.Memberships.GroupCode.Set(group.Code), linkqueries.Memberships.Role.Set("admin")}
				ctx := linkqueries.WithTrace(t.Context(), trace)
				if mode == "creation_scope" {
					_, err := orm.FirstOrCreate(ctx, tx, q.Query().Where(fields.Role.Eq("different")), draft...)
					if !errors.Is(err, fault.Invalid) {
						return errors.New("creation escaped its lookup predicates")
					}
					if count, err := q.Query().Count(t.Context(), tx); err != nil || count != 0 {
						return errors.New("failed lookup postcondition retained an insert")
					}
					return nil
				}
				current, err := q.Insert(t.Context(), tx, draft...)
				if err != nil {
					return err
				}
				if mode == "after_hook" {
					trace.VetoAt = "local.saved"
				}
				_, err = orm.UpdateOrCreate(ctx, tx, q.Query().Where(fields.Key().Eq(current.ID)), []orm.Assignment[linkqueries.Membership]{}, func(context.Context, *database.Tx, linkqueries.Membership) ([]orm.Assignment[linkqueries.Membership], error) {
					if mode == "callback" {
						return []orm.Assignment[linkqueries.Membership]{}, linkqueries.ErrVeto
					}
					return []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.Role.Set("changed")}, nil
				})
				if !errors.Is(err, linkqueries.ErrVeto) {
					return errors.New("lookup write lost callback or hook failure")
				}
				stored, err := q.Require(t.Context(), tx, current.ID)
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
		draft := []orm.Assignment[linkqueries.Membership]{linkqueries.Memberships.MemberID.Set(member.ID), linkqueries.Memberships.GroupCode.Set(group.Code), linkqueries.Memberships.Role.Set("same")}
		if _, err := q.InsertMany(t.Context(), tx, [][]orm.Assignment[linkqueries.Membership]{draft, draft}); err != nil {
			return err
		}
		first, err := q.Query().Require(t.Context(), tx)
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
		if _, err := q.Delete(t.Context(), tx, first.ID); err != nil {
			return err
		}
		found, err = orm.FirstOrCreate(t.Context(), tx, q.Query().OnlyTrashed())
		if err != nil {
			return err
		}
		if found.ID != first.ID || found.DeletedAt.IsNull() {
			return errors.New("lookup ignored explicit trashed visibility")
		}
		replacement, err := orm.FirstOrCreate(t.Context(), tx, q.Query().Where(fields.Key().Eq(first.ID)), append(slices.Clone(draft), linkqueries.Memberships.Key().Set(first.ID))...)
		if !errors.Is(err, database.UniqueViolation) || replacement.ID != (linkqueries.Membership{}).ID {
			return errors.New("hidden deleted key did not retain ordinary uniqueness failure")
		}
		if count, err := q.Query().WithTrashed().Count(t.Context(), tx); err != nil || count != 2 {
			return errors.New("lookup conflict damaged the parent transaction")
		}
		// Natural identities stay concrete and an existing row does not need its creation key again.
		natural, err := orm.FirstOrCreate(t.Context(), tx, linkqueries.Groups.Query().Where(linkqueries.Groups.Key().Eq(group.Code)))
		if err != nil {
			return err
		}
		if natural.Code != group.Code {
			return errors.New("natural lookup identity changed")
		}
		return nil
	})
}
