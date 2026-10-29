package softqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"time"
)

// MemberTable declares stored columns and their write roles. Example: orm.KeyEq(Members.Key(), key).
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	Name       orm.InputText[Member, string, string]
	ParentID   orm.NullableColumn[Member, model.ID[Member]]
	CreatedAt  orm.CreatedAt[Member, time.Time]
	UpdatedAt  orm.UpdatedAt[Member, temporal.DateTime]
	DeletedAt  orm.SoftDelete[Member, time.Time]
	Parent     orm.One[Member, Member]
	Children   orm.Many[Member, Member]
	Groups     orm.Through[Member, Group, Membership]
	ChildCount orm.Value[Member, int64]
	GroupCount orm.Value[Member, int64]
	Soft       orm.SoftDeletable[Member]
}

// Members preserves the persisted soft_members model. Example: orm.All(ctx, db, Members.Query()).
var Members = orm.Define("soft_members", func(b orm.Builder[Member], m *Member) MemberTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return MemberTable{Table: orm.TableOf(b, &m.ID),
		Name:       orm.InputTextOf[Member, string](b, &m.Name, nil),
		ParentID:   orm.NullableColumnOf(b, &m.ParentID),
		CreatedAt:  orm.CreatedAtOf(b, &m.CreatedAt),
		UpdatedAt:  orm.UpdatedAtOf(b, &m.UpdatedAt),
		DeletedAt:  deleted,
		Parent:     orm.OneOf(b, &m.Parent),
		Children:   orm.ManyOf(b, &m.Children),
		Groups:     orm.ThroughOf(b, &m.Groups),
		ChildCount: orm.ValueOf(b, &m.ChildCount),
		GroupCount: orm.ValueOf(b, &m.GroupCount),
		Soft:       orm.SoftDeletes(deleted),
	}
}, orm.WithHooks(ormMemberHooks))

// GroupTable declares stored columns and their write roles. Example: orm.KeyEq(Groups.Key(), key).
type GroupTable struct {
	orm.Table[Group, GroupCode]
	Name      orm.Text[Group, string]
	DeletedAt orm.SoftDelete[Group, temporal.DateTime]
	Soft      orm.SoftDeletable[Group]
}

// Groups preserves the persisted soft_groups model. Example: orm.All(ctx, db, Groups.Query()).
var Groups = orm.Define("soft_groups", func(b orm.Builder[Group], m *Group) GroupTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return GroupTable{Table: orm.TableOf(b, &m.Code),
		Name:      orm.TextOf(b, &m.Name),
		DeletedAt: deleted,
		Soft:      orm.SoftDeletes(deleted),
	}
})

// MembershipTable declares stored columns and their write roles. Example: orm.KeyEq(Memberships.Key(), key).
type MembershipTable struct {
	orm.Table[Membership, model.ID[Membership]]
	MemberID  orm.Column[Membership, model.ID[Member]]
	GroupCode orm.Text[Membership, GroupCode]
	DeletedAt orm.SoftDelete[Membership, time.Time]
	Soft      orm.SoftDeletable[Membership]
}

// Memberships preserves the persisted soft_memberships model. Example: orm.All(ctx, db, Memberships.Query()).
var Memberships = orm.Define("soft_memberships", func(b orm.Builder[Membership], m *Membership) MembershipTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return MembershipTable{Table: orm.TableOf(b, &m.ID),
		MemberID:  orm.ColumnOf(b, &m.MemberID),
		GroupCode: orm.TextOf(b, &m.GroupCode),
		DeletedAt: deleted,
		Soft:      orm.SoftDeletes(deleted),
	}
})

// MemberLinks declares parent, children and pivot targets. Example: Members.Query().With(MemberLinks.Parent).
var MemberLinks = struct {
	Parent   orm.BelongsTo[Member, Member]
	Children orm.HasMany[Member, Member]
	Groups   orm.ThroughRelation[Member, Group, Membership]
}{orm.BelongsToOf(Members.Parent, orm.NullableColumnExpr(Members.ParentID), orm.KeyExpr(Members.Key())), orm.HasManyOf(Members.Children, orm.KeyExpr(Members.Key()), orm.NullableColumnExpr(Members.ParentID)), orm.ManyToManyOf(Members.Groups, orm.KeyExpr(Members.Key()), orm.ColumnExpr(Memberships.MemberID), orm.TextExpr(Memberships.GroupCode), orm.KeyExpr(Groups.Key()))}

// MemberCounts binds aggregate slots. Example: Members.Query().With(MemberCounts.Children).
var MemberCounts = struct {
	Children orm.AggregateRelation[Member, int64]
	Groups   orm.AggregateRelation[Member, int64]
}{Members.ChildCount.Using(expr.Related(MemberLinks.Children, expr.Count[Member]())), Members.GroupCount.Using(expr.Related(MemberLinks.Groups, expr.Count[Group]()))}
