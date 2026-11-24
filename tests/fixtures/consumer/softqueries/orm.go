package softqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

// MemberLinks declares parent, children and pivot targets. Example: Members.Query().With(MemberLinks.Parent).
var MemberLinks = struct {
	Parent   orm.BelongsTo[Member, Member]
	Children orm.HasMany[Member, Member]
	Groups   orm.ThroughRelation[Member, Group, Membership]
}{orm.BelongsToOf(Members.Parent, Members.ParentID, Members.Key()), orm.HasManyOf(Members.Children, Members.Key(), Members.ParentID), orm.ManyToManyOf(Members.Groups, orm.JoinKeys(Members.Key(), Memberships.MemberID), orm.JoinKeys(Memberships.GroupCode, Groups.Key()))}

// MemberCounts binds aggregate slots. Example: Members.Query().With(MemberCounts.Children).
var MemberCounts = struct {
	Children orm.AggregateRelation[Member, int64]
	Groups   orm.AggregateRelation[Member, int64]
}{Members.ChildCount.Using(expr.Related(MemberLinks.Children, expr.Count[Member]())), Members.GroupCount.Using(expr.Related(MemberLinks.Groups, expr.Count[Group]()))}
