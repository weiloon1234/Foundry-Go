package linkqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestPostgresRelationStoredNaturalAndNullableKeys(t *testing.T) {
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		ctx := t.Context()
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		members, groups := linkqueries.Members, linkqueries.Groups
		if _, err := members.Update(ctx, tx, member.ID, linkqueries.Members.Name.Set("current-member")); err != nil {
			return err
		}
		if _, err := groups.Update(ctx, tx, group.Code, linkqueries.Groups.Name.Set("current-group")); err != nil {
			return err
		}
		relations := linkqueries.MemberLinks
		pivot, err := orm.Attach(ctx, tx, relations.NamedGroups, member, group)
		if err != nil {
			return err
		}
		if pivot.MemberName != "current-member" || pivot.GroupName != "current-group" {
			return errors.New("relation attachment trusted stale stored key fields")
		}
		duplicate, err := groups.Insert(ctx, tx, linkqueries.Groups.Key().Set("duplicate"), linkqueries.Groups.Name.Set("current-group"))
		if err != nil {
			return err
		}
		if _, err := orm.Attach(ctx, tx, relations.NamedGroups, member, group); !errors.Is(err, database.TooManyRows) {
			return errors.New("ambiguous target key acquired a pivot")
		}
		if _, err := groups.Update(ctx, tx, duplicate.Code, linkqueries.Groups.Name.Set("different-group")); err != nil {
			return err
		}
		if _, err := orm.Attach(ctx, tx, relations.AliasGroups, member, group); !errors.Is(err, fault.Missing) {
			return errors.New("NULL endpoint key acquired a link")
		}
		if _, err := members.Update(ctx, tx, member.ID, linkqueries.Members.Alias.Set("alias")); err != nil {
			return err
		}
		nullable, err := orm.Attach(ctx, tx, relations.AliasGroups, member, group)
		if err != nil {
			return err
		}
		alias, hasAlias := nullable.MemberAlias.Get()
		name, hasName := nullable.GroupName.Get()
		if !hasAlias || alias != "alias" || !hasName || name != "current-group" {
			return errors.New("nullable pivot keys lost their stored values")
		}
		removed, err := orm.Detach(ctx, tx, relations.AliasGroups, member, group)
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
	runORMLinks(t, func(tx *database.Tx, _ *testkit.Clock) error {
		ctx := t.Context()
		member, group, err := ormEndpoints(t, tx)
		if err != nil {
			return err
		}
		members, groups := linkqueries.Members, linkqueries.Groups
		friend, err := members.Insert(ctx, tx, linkqueries.Members.Name.Set("friend"))
		if err != nil {
			return err
		}
		r := linkqueries.MemberLinks
		edge, err := orm.Attach(ctx, wrappedTransactor{tx}, r.Friends, member, friend)
		if err != nil {
			return err
		}
		if edge.FromID != member.ID || edge.ToID != friend.ID {
			return errors.New("self relation reversed its endpoints")
		}
		none, err := orm.Detach(ctx, tx, r.Friends, friend, member)
		if err != nil {
			return err
		}
		if len(none) != 0 {
			return errors.New("reverse self relation removed an unrelated link")
		}
		detached, err := orm.Detach(ctx, wrappedTransactor{tx}, r.Friends, member, friend)
		if err != nil {
			return err
		}
		if len(detached) != 1 || detached[0].ID != edge.ID {
			return errors.New("physical self detach lost its stored pivot")
		}
		if _, err := groups.Delete(ctx, tx, group.Code); err != nil {
			return err
		}
		if _, err := orm.Attach(ctx, tx, r.Groups, member, group, linkqueries.Memberships.Role.Set("admin")); !errors.Is(err, database.NotFound) {
			return errors.New("attachment ignored deleted target visibility")
		}
		if _, err := orm.Attach(ctx, tx, r.Groups.WithTrashed(), member, group, linkqueries.Memberships.Role.Set("admin")); err != nil {
			return err
		}
		if removed, err := orm.Detach(ctx, tx, r.Groups.WithTrashed(), member, group); err != nil || len(removed) != 1 {
			return errors.New("explicit deleted-target scope was lost")
		}
		if _, err := members.Delete(ctx, tx, member.ID); err != nil {
			return err
		}
		if _, err := orm.Attach(ctx, tx, r.Groups.WithTrashed(), member, group, linkqueries.Memberships.Role.Set("admin")); !errors.Is(err, database.NotFound) {
			return errors.New("attachment accepted an inactive source model")
		}
		return nil
	})
}
