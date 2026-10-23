package reads

import (
	"context"

	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Member struct {
	ID        int64
	TenantID  int64
	Name      string
	ManagerID value.Nullable[int64]
	Manager   relation.One[Member]
	Groups    relation.Through[Group, Membership]
}
type Group struct {
	ID       int64
	TenantID int64
	Name     string
}
type Membership struct {
	ID       int64
	TenantID int64
	MemberID int64
	GroupID  int64
}
type MemberTable struct {
	orm.Table[Member, int64]
	ID        orm.Key[Member, int64]
	TenantID  orm.Number[Member, int64]
	Name      orm.Text[Member, string]
	ManagerID orm.NullableNumber[Member, int64]
	Manager   orm.One[Member, Member]
	Groups    orm.Through[Member, Group, Membership]
}
type GroupTable struct {
	orm.Table[Group, int64]
	ID       orm.Key[Group, int64]
	TenantID orm.Number[Group, int64]
	Name     orm.Text[Group, string]
}
type MembershipTable struct {
	orm.Table[Membership, int64]
	ID       orm.Key[Membership, int64]
	TenantID orm.Number[Membership, int64]
	MemberID orm.Number[Membership, int64]
	GroupID  orm.Number[Membership, int64]
}

var Members = orm.Define("read_members", func(b orm.Builder[Member], m *Member) MemberTable {
	table := orm.TableOf(b, &m.ID)
	return MemberTable{table, table.Key(), orm.NumberOf(b, &m.TenantID), orm.TextOf(b, &m.Name), orm.NullableNumberOf(b, &m.ManagerID), orm.OneOf(b, &m.Manager), orm.ThroughOf(b, &m.Groups)}
}, orm.WithRetrievalHooks(LocalReads), orm.WithGlobalScopes(func(t MemberTable) []orm.GlobalScope[Member] { return []orm.GlobalScope[Member]{MemberTenant(t)} }))
var Groups = orm.Define("read_groups", func(b orm.Builder[Group], m *Group) GroupTable {
	table := orm.TableOf(b, &m.ID)
	return GroupTable{table, table.Key(), orm.NumberOf(b, &m.TenantID), orm.TextOf(b, &m.Name)}
}, orm.WithGlobalScopes(func(t GroupTable) []orm.GlobalScope[Group] { return []orm.GlobalScope[Group]{GroupTenant(t)} }))
var Memberships = orm.Define("read_memberships", func(b orm.Builder[Membership], m *Membership) MembershipTable {
	table := orm.TableOf(b, &m.ID)
	return MembershipTable{table, table.Key(), orm.NumberOf(b, &m.TenantID), orm.NumberOf(b, &m.MemberID), orm.NumberOf(b, &m.GroupID)}
}, orm.WithGlobalScopes(func(t MembershipTable) []orm.GlobalScope[Membership] {
	return []orm.GlobalScope[Membership]{MembershipTenant(t)}
}))

func MemberTenant(t MemberTable) orm.GlobalScope[Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Member], error) {
		id, err := coremodels.TenantID(ctx)
		return t.TenantID.Eq(id), err
	})
}
func GroupTenant(t GroupTable) orm.GlobalScope[Group] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Group], error) {
		id, err := coremodels.TenantID(ctx)
		return t.TenantID.Eq(id), err
	})
}
func MembershipTenant(t MembershipTable) orm.GlobalScope[Membership] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Membership], error) {
		id, err := coremodels.TenantID(ctx)
		return t.TenantID.Eq(id), err
	})
}
func LocalReads() orm.RetrievalHooks[Member] { return ReadHooks("local") }
func ReadHooks(label string) orm.RetrievalHooks[Member] {
	return orm.RetrievalHooks[Member]{Retrieved: func(ctx context.Context, _ database.Executor, m Member) error {
		coremodels.TraceLifecycle(ctx, label+".Retrieved")
		return nil
	}}
}

var Manager = orm.BelongsToOf(Members.Manager, Members.ManagerID, Members.ID)
var MemberGroups = orm.ManyToManyOf(Members.Groups, Members.ID, Memberships.MemberID, Memberships.GroupID, Groups.ID)
