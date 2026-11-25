package caching

import (
	"context"

	models "foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin invalidate-tag
// MemberChanges links member-derived cache snapshots to a typed invalidation key.
var MemberChanges = cache.DefineTag("member-changes-v1", keyspace.TextKeys[model.ID[models.Member]]())

type MemberTags = cache.Tags[model.ID[models.Member]]

func BindMemberTags(store *cache.Store) (MemberTags, error) { return MemberChanges.Bind(store) }
func ForMember(profiles Profiles, tags MemberTags, id model.ID[models.Member]) (Profiles, error) {
	return profiles.WithTags(tags.For(id))
}
func InvalidateMember(ctx context.Context, tags MemberTags, id model.ID[models.Member]) error {
	return tags.Invalidate(ctx, id)
}

// doc-example:end invalidate-tag
