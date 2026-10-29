package retrievalqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberTable declares stored columns and their write roles. Example: orm.KeyEq(Members.Key(), key).
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	Name     orm.InputText[Member, string, string]
	Nickname orm.NullableText[Member, string]
	ParentID orm.NullableColumn[Member, model.ID[Member]]
	Parent   orm.One[Member, Member]
	Children orm.Many[Member, Member]
	Groups   orm.Through[Member, Group, Membership]
}

// Members preserves the persisted retrieval_members model. Example: orm.All(ctx, db, Members.Query()).
var Members = declareMembers()

func declareMembers() MemberTable {
	var fields MemberTable
	fields = orm.Define("retrieval_members", func(b orm.Builder[Member], m *Member) MemberTable {
		return MemberTable{Table: orm.TableOf(b, &m.ID),
			Name:     orm.InputTextOf[Member, string](b, &m.Name, nil),
			Nickname: orm.NullableTextOf(b, &m.Nickname),
			ParentID: orm.NullableColumnOf(b, &m.ParentID),
			Parent:   orm.OneOf(b, &m.Parent),
			Children: orm.ManyOf(b, &m.Children),
			Groups:   orm.ThroughOf(b, &m.Groups),
		}
	}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[Member] { return ormMemberRetrieval(fields) }))
	return fields
}

// GroupTable declares stored columns and their write roles. Example: orm.KeyEq(Groups.Key(), key).
type GroupTable struct {
	orm.Table[Group, GroupCode]
	Name orm.Text[Group, string]
}

// Groups preserves the persisted retrieval_groups model. Example: orm.All(ctx, db, Groups.Query()).
var Groups = orm.Define("retrieval_groups", func(b orm.Builder[Group], m *Group) GroupTable {
	return GroupTable{Table: orm.TableOf(b, &m.Code),
		Name: orm.TextOf(b, &m.Name),
	}
})

// MembershipTable declares stored columns and their write roles. Example: orm.KeyEq(Memberships.Key(), key).
type MembershipTable struct {
	orm.Table[Membership, model.ID[Membership]]
	MemberID  orm.Column[Membership, model.ID[Member]]
	GroupCode orm.NullableText[Membership, GroupCode]
}

// Memberships preserves the persisted retrieval_memberships model. Example: orm.All(ctx, db, Memberships.Query()).
var Memberships = orm.Define("retrieval_memberships", func(b orm.Builder[Membership], m *Membership) MembershipTable {
	return MembershipTable{Table: orm.TableOf(b, &m.ID),
		MemberID:  orm.ColumnOf(b, &m.MemberID),
		GroupCode: orm.NullableTextOf(b, &m.GroupCode),
	}
})

// MemberLinks declares parent, children and pivot targets. Example: Members.Query().With(MemberLinks.Parent).
var MemberLinks = struct {
	Parent   orm.BelongsTo[Member, Member]
	Children orm.HasMany[Member, Member]
	Groups   orm.ThroughRelation[Member, Group, Membership]
}{orm.BelongsToOf(Members.Parent, orm.NullableColumnExpr(Members.ParentID), orm.KeyExpr(Members.Key())), orm.HasManyOf(Members.Children, orm.KeyExpr(Members.Key()), orm.NullableColumnExpr(Members.ParentID)), orm.ManyToManyOf(Members.Groups, orm.KeyExpr(Members.Key()), orm.ColumnExpr(Memberships.MemberID), orm.NullableTextExpr(Memberships.GroupCode), orm.KeyExpr(Groups.Key()))}
