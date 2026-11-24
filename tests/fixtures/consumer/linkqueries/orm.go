package linkqueries

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var membershipFields MembershipTable

func init() { membershipFields = Memberships }

func membershipHooks() orm.Hooks[Membership] { return membershipORMHooks("local", membershipFields) }

// MemberLinks owns immutable relation plans. Example: Members.Query().With(MemberLinks.Groups).
var MemberLinks = struct {
	Groups      orm.ThroughRelation[Member, Group, Membership]
	NamedGroups orm.ThroughRelation[Member, Group, NaturalLink]
	AliasGroups orm.ThroughRelation[Member, Group, NullableLink]
	Friends     orm.ThroughRelation[Member, Member, Friendship]
}{
	orm.ManyToManyOf(Members.Groups, orm.JoinKeys(Members.Key(), Memberships.MemberID), orm.JoinKeys(Memberships.GroupCode, Groups.Key())),
	orm.ManyToManyOf(Members.NamedGroups, orm.JoinKeys(Members.Name, NaturalLinks.MemberName), orm.JoinKeys(NaturalLinks.GroupName, Groups.Name)),
	orm.ManyToManyOf(Members.AliasGroups, orm.JoinKeys(Members.Alias, NullableLinks.MemberAlias), orm.JoinKeys(NullableLinks.GroupName, Groups.Name)),
	orm.ManyToManyOf(Members.Friends, orm.JoinKeys(Members.Key(), Friendships.FromID), orm.JoinKeys(Friendships.ToID, Members.Key())),
}

func memberRetrievalHooks() orm.RetrievalHooks[Member] {
	return orm.RetrievalHooks[Member]{Retrieved: func(ctx context.Context, _ database.Executor, _ Member) error { recordRead(ctx); return nil }}
}

func groupRetrievalHooks() orm.RetrievalHooks[Group] {
	return orm.RetrievalHooks[Group]{Retrieved: func(ctx context.Context, _ database.Executor, _ Group) error { recordRead(ctx); return nil }}
}

func archiveHooks() orm.Hooks[Archive] {
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
}

func archiveRetrievalHooks() orm.RetrievalHooks[Archive] {
	return orm.RetrievalHooks[Archive]{Retrieved: func(ctx context.Context, _ database.Executor, _ Archive) error { recordRead(ctx); return nil }}
}
