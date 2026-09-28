package linkqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresRelationStoredNaturalAndNullableKeys(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		ctx := t.Context()
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		members, groups := linkqueries.QueryLinkMembers(), linkqueries.QueryLinkGroups()
		if _, err := members.Update(ctx, tx, member.ID, linkqueries.MemberDraft{}.SetName("current-member")); err != nil {
			return err
		}
		if _, err := groups.Update(ctx, tx, group.Code, linkqueries.GroupDraft{}.SetName("current-group")); err != nil {
			return err
		}
		relations := linkqueries.MemberRelations()
		pivot, err := relations.NamedGroups.Attach(ctx, tx, member, group, linkqueries.NaturalLinkDraft{})
		if err != nil {
			return err
		}
		if pivot.MemberName != "current-member" || pivot.GroupName != "current-group" {
			return errors.New("relation attachment trusted stale stored key fields")
		}
		duplicate, err := groups.Create(ctx, tx, linkqueries.GroupDraft{}.SetCode("duplicate").SetName("current-group"))
		if err != nil {
			return err
		}
		if _, err := relations.NamedGroups.Attach(ctx, tx, member, group, linkqueries.NaturalLinkDraft{}); !errors.Is(err, database.TooManyRows) {
			return errors.New("ambiguous target key acquired a pivot")
		}
		if _, err := groups.Update(ctx, tx, duplicate.Code, linkqueries.GroupDraft{}.SetName("different-group")); err != nil {
			return err
		}
		if _, err := relations.AliasGroups.Attach(ctx, tx, member, group, linkqueries.NullableLinkDraft{}); !errors.Is(err, fault.Missing) {
			return errors.New("NULL endpoint key acquired a link")
		}
		if _, err := members.Update(ctx, tx, member.ID, linkqueries.MemberDraft{}.SetAlias("alias")); err != nil {
			return err
		}
		nullable, err := relations.AliasGroups.Attach(ctx, tx, member, group, linkqueries.NullableLinkDraft{})
		if err != nil {
			return err
		}
		alias, hasAlias := nullable.MemberAlias.Get()
		name, hasName := nullable.GroupName.Get()
		if !hasAlias || alias != "alias" || !hasName || name != "current-group" {
			return errors.New("nullable pivot keys lost their stored values")
		}
		removed, err := relations.AliasGroups.Detach(ctx, tx, member, group)
		if err != nil {
			return err
		}
		if len(removed) != 1 || removed[0].ID != nullable.ID {
			return errors.New("nullable-key detach lost its exact pivot")
		}
		return nil
	})
}

func TestPostgresRelationEndpointVisibilityAndSelfLinks(t *testing.T) {
	runLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		ctx := t.Context()
		member, group, err := endpoints(t, tx)
		if err != nil {
			return err
		}
		members, groups := linkqueries.QueryLinkMembers(), linkqueries.QueryLinkGroups()
		friend, err := members.Create(ctx, tx, linkqueries.MemberDraft{}.SetName("friend"))
		if err != nil {
			return err
		}
		r := linkqueries.MemberRelations()
		edge, err := r.Friends.Attach(ctx, wrappedTransactor{tx}, member, friend, linkqueries.FriendshipDraft{})
		if err != nil {
			return err
		}
		if edge.FromID != member.ID || edge.ToID != friend.ID {
			return errors.New("self relation reversed its endpoints")
		}
		none, err := r.Friends.Detach(ctx, tx, friend, member)
		if err != nil {
			return err
		}
		if len(none) != 0 {
			return errors.New("reverse self relation removed an unrelated link")
		}
		detached, err := r.Friends.Detach(ctx, wrappedTransactor{tx}, member, friend)
		if err != nil {
			return err
		}
		if len(detached) != 1 || detached[0].ID != edge.ID {
			return errors.New("physical self detach lost its stored pivot")
		}
		if _, err := groups.Delete(ctx, tx, group.Code); err != nil {
			return err
		}
		if _, err := r.Groups.Attach(ctx, tx, member, group, linkqueries.MembershipDraft{}.SetRole("admin")); !errors.Is(err, database.NotFound) {
			return errors.New("attachment ignored deleted target visibility")
		}
		if _, err := r.Groups.WithTrashed().Attach(ctx, tx, member, group, linkqueries.MembershipDraft{}.SetRole("admin")); err != nil {
			return err
		}
		if removed, err := r.Groups.WithTrashed().Detach(ctx, tx, member, group); err != nil || len(removed) != 1 {
			return errors.New("explicit deleted-target scope was lost")
		}
		if _, err := members.Delete(ctx, tx, member.ID); err != nil {
			return err
		}
		if _, err := r.Groups.WithTrashed().Attach(ctx, tx, member, group, linkqueries.MembershipDraft{}.SetRole("admin")); !errors.Is(err, database.NotFound) {
			return errors.New("attachment accepted an inactive source model")
		}
		return nil
	})
}
