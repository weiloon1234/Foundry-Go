package reads

import (
	"context"

	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=read_members var=Members key=ID retrieval=LocalReads scopes=memberScopes identity=false
type Member struct {
	ID        int64 `foundry:"member=ID,key-role=lookup"`
	TenantID  int64
	Name      string
	ManagerID value.Nullable[int64]
	Manager   relation.One[Member]
	Groups    relation.Through[Group, Membership]
}

//foundry:model table=read_groups var=Groups key=ID scopes=groupScopes identity=false
type Group struct {
	ID       int64 `foundry:"member=ID,key-role=lookup"`
	TenantID int64
	Name     string
}

//foundry:model table=read_memberships var=Memberships key=ID scopes=membershipScopes identity=false
type Membership struct {
	ID       int64 `foundry:"member=ID,key-role=lookup"`
	TenantID int64
	MemberID int64
	GroupID  int64
}

func MemberTenant(t MemberTable) orm.GlobalScope[Member] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Member], error) {
		id, err := coremodels.TenantID(ctx)
		return orm.Eq(t.TenantID, id), err
	})
}
func GroupTenant(t GroupTable) orm.GlobalScope[Group] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Group], error) {
		id, err := coremodels.TenantID(ctx)
		return orm.Eq(t.TenantID, id), err
	})
}
func MembershipTenant(t MembershipTable) orm.GlobalScope[Membership] {
	return orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[Membership], error) {
		id, err := coremodels.TenantID(ctx)
		return orm.Eq(t.TenantID, id), err
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
var MemberGroups = orm.ManyToManyOf(Members.Groups, orm.JoinKeys(Members.ID, Memberships.MemberID), orm.JoinKeys(Memberships.GroupID, Groups.ID))

func membershipScopes(t MembershipTable) []orm.GlobalScope[Membership] {
	return []orm.GlobalScope[Membership]{MembershipTenant(t)}
}

func memberScopes(t MemberTable) []orm.GlobalScope[Member] {
	return []orm.GlobalScope[Member]{MemberTenant(t)}
}

func groupScopes(t GroupTable) []orm.GlobalScope[Group] {
	return []orm.GlobalScope[Group]{GroupTenant(t)}
}
