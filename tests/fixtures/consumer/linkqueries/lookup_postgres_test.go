package linkqueries_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresLookupWritesChooseOneLifecycleBranch(t *testing.T) {
	trace := &linkqueries.Trace{}
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		ctx := linkqueries.WithTrace(t.Context(), trace)
		q := linkqueries.QueryLinkMemberships()
		fields := linkqueries.MembershipFields()
		draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole(" ADMIN ")
		selected := q.Where(fields.Role.Eq("admin"))
		created, err := selected.FirstOrCreate(ctx, wrappedTransactor{tx}, draft)
		if err != nil {
			return err
		}
		if created.Role != "admin" || len(trace.Steps) != 8 || trace.Reads != 0 {
			return errors.New("lookup creation bypassed normal lifecycle or dispatched internal retrieval")
		}
		trace.Steps = nil
		// This draft cannot create a valid membership; a found branch must leave it unused.
		found, err := selected.FirstOrCreate(ctx, tx, linkqueries.MembershipDraft{})
		if err != nil {
			return err
		}
		if found.ID != created.ID || len(trace.Steps) != 0 || trace.Reads != 0 {
			return errors.New("found lookup prepared creation or ran model callbacks")
		}
		probe := &unusedMembershipCreation{}
		observed, err := selected.Query.FirstOrInsert(ctx, tx, probe)
		if err != nil || observed.ID != created.ID || probe.calls != 0 {
			return errors.New("found lookup prepared its unused creation bridge")
		}
		if _, err := q.Where(fields.Role.Eq("absent")).Query.FirstOrInsert(ctx, tx, probe); !errors.Is(err, linkqueries.ErrVeto) || probe.calls != 1 {
			return errors.New("missing lookup did not prepare exactly one creation bridge")
		}
		calls := 0
		update := func(ctx context.Context, owner *database.Tx, current linkqueries.Membership) (linkqueries.MembershipDraft, error) {
			calls++
			if current.ID != created.ID {
				return linkqueries.MembershipDraft{}, errors.New("lookup callback received wrong stored model")
			}
			if count, err := q.Count(ctx, owner); err != nil || count != 1 {
				return linkqueries.MembershipDraft{}, errors.New("lookup callback lost its usable transaction")
			}
			return linkqueries.MembershipDraft{}.SetRole(" EDITOR "), nil
		}
		updated, err := selected.UpdateOrCreate(ctx, wrappedTransactor{tx}, linkqueries.MembershipDraft{}, update)
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
		missing := q.Where(fields.Role.Eq("new"))
		next, err := missing.UpdateOrCreate(ctx, tx, draft.SetRole(" NEW "), func(context.Context, *database.Tx, linkqueries.Membership) (linkqueries.MembershipDraft, error) {
			calls++
			return linkqueries.MembershipDraft{}, errors.New("missing branch invoked update callback")
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
			runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
				member, group, err := endpoints(t, tx)
				if err != nil {
					return err
				}
				q := linkqueries.QueryLinkMemberships()
				fields := linkqueries.MembershipFields()
				draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole("admin")
				ctx := linkqueries.WithTrace(t.Context(), trace)
				if mode == "creation_scope" {
					_, err := q.Where(fields.Role.Eq("different")).FirstOrCreate(ctx, tx, draft)
					if !errors.Is(err, fault.Invalid) {
						return errors.New("creation escaped its lookup predicates")
					}
					if count, err := q.Count(t.Context(), tx); err != nil || count != 0 {
						return errors.New("failed lookup postcondition retained an insert")
					}
					return nil
				}
				current, err := q.Create(t.Context(), tx, draft)
				if err != nil {
					return err
				}
				if mode == "after_hook" {
					trace.VetoAt = "local.saved"
				}
				_, err = q.Where(fields.ID.Eq(current.ID)).UpdateOrCreate(ctx, tx, linkqueries.MembershipDraft{}, func(context.Context, *database.Tx, linkqueries.Membership) (linkqueries.MembershipDraft, error) {
					if mode == "callback" {
						return linkqueries.MembershipDraft{}, linkqueries.ErrVeto
					}
					return linkqueries.MembershipDraft{}.SetRole("changed"), nil
				})
				if !errors.Is(err, linkqueries.ErrVeto) {
					return errors.New("lookup write lost callback or hook failure")
				}
				stored, err := q.RequireFind(t.Context(), tx, current.ID)
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
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		q := linkqueries.QueryLinkMemberships()
		fields := linkqueries.MembershipFields()
		draft := linkqueries.MembershipDraft{}.SetMemberID(member.ID).SetGroupCode(group.Code).SetRole("same")
		if _, err := q.CreateMany(t.Context(), tx, []linkqueries.MembershipDraft{draft, draft}); err != nil {
			return err
		}
		first, err := q.RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		found, err := q.FirstOrCreate(t.Context(), tx, linkqueries.MembershipDraft{})
		if err != nil {
			return err
		}
		if found.ID != first.ID {
			return errors.New("lookup did not use ordinary first primary ordering")
		}
		if _, err := q.Delete(t.Context(), tx, first.ID); err != nil {
			return err
		}
		found, err = q.OnlyTrashed().FirstOrCreate(t.Context(), tx, linkqueries.MembershipDraft{})
		if err != nil {
			return err
		}
		if found.ID != first.ID || found.DeletedAt.IsNull() {
			return errors.New("lookup ignored explicit trashed visibility")
		}
		replacement, err := q.Where(fields.ID.Eq(first.ID)).FirstOrCreate(t.Context(), tx, draft.SetID(first.ID))
		if !errors.Is(err, database.UniqueViolation) || replacement.ID != (linkqueries.Membership{}).ID {
			return errors.New("hidden deleted key did not retain ordinary uniqueness failure")
		}
		if count, err := q.WithTrashed().Count(t.Context(), tx); err != nil || count != 2 {
			return errors.New("lookup conflict damaged the parent transaction")
		}
		// Natural identities stay concrete and an existing row does not need its creation key again.
		natural, err := linkqueries.QueryLinkGroups().Where(linkqueries.GroupFields().Code.Eq(group.Code)).FirstOrCreate(t.Context(), tx, linkqueries.GroupDraft{})
		if err != nil {
			return err
		}
		if natural.Code != group.Code {
			return errors.New("natural lookup identity changed")
		}
		return nil
	})
}

// The public runtime draft bridge makes lazy branch preparation observable
// without replacing UUID entropy or invoking a real mutation on the found path.
type unusedMembershipCreation struct{ calls int }

func (d *unusedMembershipCreation) FoundryCreateMutation(query.Mutation[linkqueries.Membership]) (query.Mutation[linkqueries.Membership], error) {
	d.calls++
	return query.Mutation[linkqueries.Membership]{}, linkqueries.ErrVeto
}
