package retrievalqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var memberFields MemberTable

func init() { memberFields = Members }

func memberRetrievalHooks() orm.RetrievalHooks[Member] { return ormMemberRetrieval(memberFields) }

// MemberLinks declares parent, children and pivot targets. Example: Members.Query().With(MemberLinks.Parent).
var MemberLinks = struct {
	Parent   orm.BelongsTo[Member, Member]
	Children orm.HasMany[Member, Member]
	Groups   orm.ThroughRelation[Member, Group, Membership]
}{orm.BelongsToOf(Members.Parent, Members.ParentID, Members.Key()), orm.HasManyOf(Members.Children, Members.Key(), Members.ParentID), orm.ManyToManyOf(Members.Groups, orm.JoinKeys(Members.Key(), Memberships.MemberID), orm.JoinKeys(Memberships.GroupCode, Groups.Key()))}
