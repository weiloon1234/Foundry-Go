package caching

import (
	"context"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberChanges links member-derived cache snapshots to a typed invalidation key.
var MemberChanges = cache.DefineTag("member-changes-v1", cache.TextKeys[model.ID[mutatorqueries.Member]]())

type MemberTags = cache.Tags[model.ID[mutatorqueries.Member]]

func BindMemberTags(store *cache.Store) (MemberTags, error) { return MemberChanges.Bind(store) }
func ForMember(profiles Profiles, tags MemberTags, id model.ID[mutatorqueries.Member]) (Profiles, error) {
	return profiles.WithTags(tags.For(id))
}
func InvalidateMember(ctx context.Context, tags MemberTags, id model.ID[mutatorqueries.Member]) error {
	return tags.Invalidate(ctx, id)
}
