package linkqueries

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// MemberTable declares stored columns and relation slots. Example: Members.Name.Eq("Ada").
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	ID          orm.Primary[Member, model.ID[Member]]
	Name        orm.Text[Member, string]
	Alias       orm.NullableText[Member, string]
	DeletedAt   orm.SoftDelete[Member, time.Time]
	Soft        orm.SoftDeletable[Member]
	Groups      orm.Through[Member, Group, Membership]
	NamedGroups orm.Through[Member, Group, NaturalLink]
	AliasGroups orm.Through[Member, Group, NullableLink]
	Friends     orm.Through[Member, Member, Friendship]
}

// Members preserves the member declaration and retrieval callback. Example: Members.Find(ctx,db,id).
var Members = orm.Define("link_members", func(b orm.Builder[Member], m *Member) MemberTable {
	table := orm.TableOf(b, &m.ID)
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return MemberTable{Table: table, ID: orm.PrimaryOf(b, table.Key()), Name: orm.TextOf(b, &m.Name), Alias: orm.NullableTextOf(b, &m.Alias), DeletedAt: deleted, Soft: orm.SoftDeletes(deleted), Groups: orm.ThroughOf(b, &m.Groups), NamedGroups: orm.ThroughOf(b, &m.NamedGroups), AliasGroups: orm.ThroughOf(b, &m.AliasGroups), Friends: orm.ThroughOf(b, &m.Friends)}
}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[Member] {
	return orm.RetrievalHooks[Member]{Retrieved: func(ctx context.Context, _ database.Executor, _ Member) error { recordRead(ctx); return nil }}
}))

// GroupTable retains the named natural primary key. Example: Groups.Key().Eq(GroupCode("staff")).
type GroupTable struct {
	orm.Table[Group, GroupCode]
	Name      orm.Text[Group, string]
	DeletedAt orm.SoftDelete[Group, temporal.DateTime]
	Soft      orm.SoftDeletable[Group]
}

// Groups exposes stored group values. Example: Groups.Query().All(ctx,db).
var Groups = orm.Define("link_groups", func(b orm.Builder[Group], m *Group) GroupTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return GroupTable{orm.TableOf(b, &m.Code), orm.TextOf(b, &m.Name), deleted, orm.SoftDeletes(deleted)}
}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[Group] {
	return orm.RetrievalHooks[Group]{Retrieved: func(ctx context.Context, _ database.Executor, _ Group) error { recordRead(ctx); return nil }}
}))

// MembershipTable keeps pivot inputs and managed timestamp roles. Example: Memberships.Role.Set("staff").
type MembershipTable struct {
	orm.Table[Membership, model.ID[Membership]]
	MemberID  orm.Column[Membership, model.ID[Member]]
	GroupCode orm.Text[Membership, GroupCode]
	Role      orm.InputText[Membership, string, string]
	CreatedAt orm.CreatedAt[Membership, time.Time]
	UpdatedAt orm.UpdatedAt[Membership, temporal.DateTime]
	DeletedAt orm.SoftDelete[Membership, time.Time]
	Soft      orm.SoftDeletable[Membership]
}

// Memberships installs one per-write local factory. Example: Memberships.Query().All(ctx,db).
var Memberships = declareMemberships()

func declareMemberships() MembershipTable {
	var fields MembershipTable
	fields = orm.Define("link_memberships", func(b orm.Builder[Membership], m *Membership) MembershipTable {
		deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
		return MembershipTable{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.MemberID), orm.TextOf(b, &m.GroupCode), orm.InputTextOf[Membership, string](b, &m.Role, nil), orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt), deleted, orm.SoftDeletes(deleted)}
	}, orm.WithHooks(func() orm.Hooks[Membership] { return membershipORMHooks("local", fields) }))
	return fields
}

// NaturalLinkTable declares non-primary pivot keys. Example: NaturalLinks.MemberName.Eq("Ada").
type NaturalLinkTable struct {
	orm.Table[NaturalLink, model.ID[NaturalLink]]
	MemberName orm.Text[NaturalLink, string]
	GroupName  orm.Text[NaturalLink, string]
}

// NaturalLinks binds the natural-key pivot. Example: NaturalLinks.Query().All(ctx,db).
var NaturalLinks = orm.Define("link_natural", func(b orm.Builder[NaturalLink], m *NaturalLink) NaturalLinkTable {
	return NaturalLinkTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.MemberName), orm.TextOf(b, &m.GroupName)}
})

// NullableLinkTable preserves nullable link keys. Example: NullableLinks.MemberAlias.IsNull().
type NullableLinkTable struct {
	orm.Table[NullableLink, model.ID[NullableLink]]
	MemberAlias orm.NullableText[NullableLink, string]
	GroupName   orm.NullableText[NullableLink, string]
}

// NullableLinks binds nullable pivot fields. Example: NullableLinks.Query().All(ctx,db).
var NullableLinks = orm.Define("link_nullable", func(b orm.Builder[NullableLink], m *NullableLink) NullableLinkTable {
	return NullableLinkTable{orm.TableOf(b, &m.ID), orm.NullableTextOf(b, &m.MemberAlias), orm.NullableTextOf(b, &m.GroupName)}
})

// FriendshipTable retains both model-owned endpoints. Example: Friendships.FromID.Eq(member.ID).
type FriendshipTable struct {
	orm.Table[Friendship, model.ID[Friendship]]
	FromID orm.Column[Friendship, model.ID[Member]]
	ToID   orm.Column[Friendship, model.ID[Member]]
}

// Friendships binds the self-relation pivot. Example: Friendships.Query().All(ctx,db).
var Friendships = orm.Define("link_friendships", func(b orm.Builder[Friendship], m *Friendship) FriendshipTable {
	return FriendshipTable{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.FromID), orm.ColumnOf(b, &m.ToID)}
})

// MemberLinks owns immutable relation plans. Example: Members.Query().With(MemberLinks.Groups).
var MemberLinks = struct {
	Groups      orm.ThroughRelation[Member, Group, Membership]
	NamedGroups orm.ThroughRelation[Member, Group, NaturalLink]
	AliasGroups orm.ThroughRelation[Member, Group, NullableLink]
	Friends     orm.ThroughRelation[Member, Member, Friendship]
}{
	orm.ManyToManyOf(Members.Groups, Members.Key(), Memberships.MemberID, Memberships.GroupCode, Groups.Key()),
	orm.ManyToManyOf(Members.NamedGroups, Members.Name, NaturalLinks.MemberName, NaturalLinks.GroupName, Groups.Name),
	orm.ManyToManyOf(Members.AliasGroups, Members.Alias, NullableLinks.MemberAlias, NullableLinks.GroupName, Groups.Name),
	orm.ManyToManyOf(Members.Friends, Members.Key(), Friendships.FromID, Friendships.ToID, Members.Key()),
}

// ArchiveTable preserves database defaults and distinct mutation input. Example: Archives.Tag.Set(ArchiveTagInput{Text:"new"}).
type ArchiveTable struct {
	orm.Table[Archive, model.ID[Archive]]
	ID        orm.Primary[Archive, model.ID[Archive]]
	MemberID  orm.Column[Archive, model.ID[Member]]
	Name      orm.Text[Archive, string]
	Alias     orm.NullableText[Archive, string]
	Tag       orm.InputText[Archive, ArchiveTagInput, string]
	Counter   orm.Number[Archive, int64]
	Amount    orm.Number[Archive, decimal.Decimal]
	Payload   orm.JSON[Archive, []string]
	CreatedAt orm.CreatedAt[Archive, time.Time]
	UpdatedAt orm.UpdatedAt[Archive, temporal.DateTime]
}

// Archives supports typed insert-select mappings for its primary key. Example: Archives.Counter.Set(0).
var Archives = orm.Define("link_archives", func(b orm.Builder[Archive], m *Archive) ArchiveTable {
	table := orm.TableOf(b, &m.ID)
	return ArchiveTable{table, orm.PrimaryOf(b, table.Key()), orm.ColumnOf(b, &m.MemberID), orm.TextOf(b, &m.Name), orm.NullableTextOf(b, &m.Alias), orm.InputTextOf[Archive, ArchiveTagInput](b, &m.Tag, nil), orm.NumberOf(b, &m.Counter), orm.NumberOf(b, &m.Amount), orm.JSONOf(b, &m.Payload), orm.CreatedAtOf(b, &m.CreatedAt), orm.UpdatedAtOf(b, &m.UpdatedAt)}
}, orm.WithHooks(func() orm.Hooks[Archive] {
	return orm.Hooks[Archive]{Creating: func(ctx context.Context, _ *database.Tx, draft *orm.Write[Archive]) error {
		if trace := traceFrom(ctx); trace != nil {
			if trace.InspectArchive != nil {
				if err := trace.InspectArchive(draft); err != nil {
					return err
				}
			}
			trace.Steps = append(trace.Steps, "archive.creating")
			return ErrVeto
		}
		return nil
	}}
}), orm.WithRetrievalHooks(func() orm.RetrievalHooks[Archive] {
	return orm.RetrievalHooks[Archive]{Retrieved: func(ctx context.Context, _ database.Executor, _ Archive) error { recordRead(ctx); return nil }}
}))
