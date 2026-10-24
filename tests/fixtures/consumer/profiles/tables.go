package profiles

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// ProfileTable binds the stored profile fields. Example: Profiles.Query().
type ProfileTable struct {
	// Table owns the concrete UUID key. Example: Profiles.Key().Eq(id).
	orm.Table[Profile, model.ID[Profile]]
	// Name is the stored display name. Example: Profiles.Name.Set("Consumer").
	Name orm.Text[Profile, string]
	// DeletedAt stores soft deletion; Restore clears it. Example: Profiles.DeletedAt.IsNull().
	DeletedAt orm.SoftDelete[Profile, temporal.DateTime]
	// Soft authorizes physical deletion. Example: Profiles.ForceDelete(ctx,tx,Profiles.Soft,id).
	Soft orm.SoftDeletable[Profile]
}

// Profiles is the handwritten profile declaration. Example: Profiles.Insert(ctx,tx,Profiles.Name.Set("Consumer")).
var Profiles = orm.Define("profiles", func(b orm.Builder[Profile], m *Profile) ProfileTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return ProfileTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name), deleted, orm.SoftDeletes(deleted)}
})

// ProfileIdentity preserves the existing persisted identity. Example: ProfileIdentity.ReferenceOf(profile).
var ProfileIdentity = orm.IdentityOf(Profiles.Table, Profiles.Key())

// Tables publishes the profile declaration to application assembly. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Profiles} }
