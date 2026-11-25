package relationshipfilters

import (
	"context"

	models "foundry.test/consumer/linkqueries"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func attachMembership(ctx context.Context, db *database.DB, member models.Member, group models.Group) error {
	// doc-example:begin model-relation-writes-1
	groups := models.MemberLinks.Groups
	membership, err := orm.Attach(ctx, db, groups, member, group,
		orm.Set(models.Memberships.Role, " ADMIN "))
	if err != nil {
		return err
	}
	// doc-example:end model-relation-writes-1
	_ = membership
	return err
}

func detachMembership(ctx context.Context, db *database.DB, member models.Member, group models.Group) error {
	// doc-example:begin model-relation-writes-2
	groups := models.MemberLinks.Groups
	removed, err := orm.Detach(ctx, db, groups, member, group)
	if err != nil {
		return err
	}
	permanentlyRemoved, err := orm.ForceDetach(ctx, db, groups.WithTrashedPivot(),
		models.Memberships.Soft, member, group)
	if err != nil {
		return err
	}
	// doc-example:end model-relation-writes-2
	_ = removed
	_ = permanentlyRemoved
	return err
}

func detachAdminMembership(ctx context.Context, db *database.DB, member models.Member, group models.Group) error {
	// doc-example:begin model-relation-writes-4
	adminGroups := models.MemberLinks.Groups.
		Where(orm.Eq(models.Groups.Name, "staff")).
		WherePivot(orm.Eq(models.Memberships.Role, "admin"))
	adminGroups = orm.WithRelationWriteLimit(adminGroups, 50)
	removed, err := orm.Detach(ctx, db, adminGroups, member, group)
	if err != nil {
		return err
	}
	// doc-example:end model-relation-writes-4
	_ = removed
	return err
}
