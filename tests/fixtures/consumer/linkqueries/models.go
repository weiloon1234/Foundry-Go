// Package linkqueries exercises typed relation writes through pivot lifecycle.
package linkqueries

import (
	"context"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=link_members retrieval=memberReads
type Member struct {
	ID    model.ID[Member]
	Name  string
	Alias value.Nullable[string]
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt   value.Nullable[time.Time]
	Groups      relation.Through[Group, Membership]
	NamedGroups relation.Through[Group, NaturalLink]
	AliasGroups relation.Through[Group, NullableLink]
	Friends     relation.Through[Member, Friendship]
}

type GroupCode string

//foundry:model table=link_groups primary=Code retrieval=groupReads
type Group struct {
	Code GroupCode
	Name string
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[temporal.DateTime]
}

//foundry:model table=link_memberships hooks=membershipHooks
type Membership struct {
	ID        model.ID[Membership]
	MemberID  model.ID[Member]
	GroupCode GroupCode
	// Foundry field behavior (generated): Membership.Role retains stored link_memberships.role. Custom setter: [Membership.MutateRole] transforms assigned values during persistence through [MembershipDraft.SetRole]. Direct field assignment and draft construction do not invoke it.
	Role string
	// Foundry field behavior (generated): Managed creation timestamp: persistence supplies the owning application clock when omitted, after before-write hooks and before field mutators. An explicit input is preserved.
	CreatedAt time.Time
	// Foundry field behavior (generated): Managed update timestamp: persistence replaces assigned or omitted input with the owning application clock after before-write hooks and before field mutators. Conflict updates copy its normalized proposed value.
	UpdatedAt temporal.DateTime
	// Foundry field behavior (generated): Managed soft-delete timestamp: Delete sets this stored field and Restore clears it. Ordinary queries exclude deleted models; WithTrashed and OnlyTrashed select visibility explicitly. Assigning this field directly through a draft uses ordinary create/update hooks rather than deletion/restoration events.
	DeletedAt value.Nullable[time.Time]
}

func (Membership) MutateRole(role string) (string, error) {
	return strings.ToLower(strings.TrimSpace(role)), nil
}

//foundry:model table=link_natural
type NaturalLink struct {
	ID         model.ID[NaturalLink]
	MemberName string
	GroupName  string
}

//foundry:model table=link_nullable
type NullableLink struct {
	ID          model.ID[NullableLink]
	MemberAlias value.Nullable[string]
	GroupName   value.Nullable[string]
}

//foundry:model table=link_friendships
type Friendship struct {
	ID     model.ID[Friendship]
	FromID model.ID[Member]
	ToID   model.ID[Member]
}

func (Member) DefineRelations() MemberRelationSet {
	m, g, p, n, z, f := MemberFields(), GroupFields(), MembershipFields(), NaturalLinkFields(), NullableLinkFields(), FriendshipFields()
	return MemberRelationSet{
		Groups:      query.ManyToMany(m.ID, p.MemberID, p.GroupCode, g.Code),
		NamedGroups: query.ManyToMany(m.Name, n.MemberName, n.GroupName, g.Name),
		AliasGroups: query.ManyToMany(m.Alias, z.MemberAlias, z.GroupName, g.Name),
		Friends:     query.ManyToMany(m.ID, f.FromID, f.ToID, m.ID),
	}
}

func memberReads() MemberRetrievalHooks {
	return MemberRetrievalHooks{Retrieved: func(ctx context.Context, _ database.Executor, _ Member) error { recordRead(ctx); return nil }}
}
func groupReads() GroupRetrievalHooks {
	return GroupRetrievalHooks{Retrieved: func(ctx context.Context, _ database.Executor, _ Group) error { recordRead(ctx); return nil }}
}
