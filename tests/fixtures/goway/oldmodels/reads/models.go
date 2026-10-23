package reads

import (
	"context"

	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=read_members primary=ID retrieval=LocalReads
type Member struct {
	ID        int64
	TenantID  int64
	Name      string
	ManagerID value.Nullable[int64]
	Manager   relation.One[Member]
	Groups    relation.Through[Group, Membership]
}

//foundry:model table=read_groups primary=ID
type Group struct {
	ID       int64
	TenantID int64
	Name     string
}

//foundry:model table=read_memberships primary=ID
type Membership struct {
	ID       int64
	TenantID int64
	MemberID int64
	GroupID  int64
}

func defineMemberRelations() MemberRelationSet {
	m, p, g := MemberFields(), MembershipFields(), GroupFields()
	return MemberRelationSet{Manager: query.BelongsTo(m.ManagerID, m.ID), Groups: query.ManyToMany(m.ID, p.MemberID, p.GroupID, g.ID)}
}
func MemberTenant() query.GlobalScope[Member] {
	return query.NewContextScope("tenant", func(ctx context.Context) (query.Predicate[Member], error) {
		id, err := coremodels.TenantID(ctx)
		return MemberFields().TenantID.Eq(id), err
	})
}
func GroupTenant() query.GlobalScope[Group] {
	return query.NewContextScope("tenant", func(ctx context.Context) (query.Predicate[Group], error) {
		id, err := coremodels.TenantID(ctx)
		return GroupFields().TenantID.Eq(id), err
	})
}
func MembershipTenant() query.GlobalScope[Membership] {
	return query.NewContextScope("tenant", func(ctx context.Context) (query.Predicate[Membership], error) {
		id, err := coremodels.TenantID(ctx)
		return MembershipFields().TenantID.Eq(id), err
	})
}
func defineMemberGlobalScopes() []query.GlobalScope[Member] {
	return []query.GlobalScope[Member]{MemberTenant()}
}
func defineGroupGlobalScopes() []query.GlobalScope[Group] {
	return []query.GlobalScope[Group]{GroupTenant()}
}
func defineMembershipGlobalScopes() []query.GlobalScope[Membership] {
	return []query.GlobalScope[Membership]{MembershipTenant()}
}
func LocalReads() MemberRetrievalHooks { return ReadHooks("local") }
func ReadHooks(label string) MemberRetrievalHooks {
	return MemberRetrievalHooks{Retrieved: func(ctx context.Context, _ database.Executor, m Member) error {
		coremodels.TraceLifecycle(ctx, label+".Retrieved")
		return nil
	}}
}
