package relationshipfilters

import (
	"context"

	models "foundry.test/consumer/linkqueries"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// doc-example:begin model-relation-writes-1
func attachMembership(ctx context.Context, db *database.DB, member models.Member, group models.Group) error {
	groups := models.MemberLinks.Groups
	membership, err := orm.Attach(ctx, db, groups, member, group,
		orm.Set(models.Memberships.Role, " ADMIN "))
	if err != nil {
		return err
	}
	_ = membership
	return err
}

// doc-example:end model-relation-writes-1

// doc-example:begin model-relation-writes-2
func detachMembership(ctx context.Context, db *database.DB, member models.Member, group models.Group) error {
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
	_ = removed
	_ = permanentlyRemoved
	return err
}

// doc-example:end model-relation-writes-2

// doc-example:begin model-relation-writes-4
func detachAdminMembership(ctx context.Context, db *database.DB, member models.Member, group models.Group) error {
	adminGroups := models.MemberLinks.Groups.
		Where(orm.Eq(models.Groups.Name, "staff")).
		WherePivot(orm.Eq(models.Memberships.Role, "admin"))
	adminGroups = orm.WithRelationWriteLimit(adminGroups, 50)
	removed, err := orm.Detach(ctx, db, adminGroups, member, group)
	if err != nil {
		return err
	}
	_ = removed
	return err
}

// doc-example:end model-relation-writes-4
