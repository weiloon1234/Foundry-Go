// Package caching verifies typed cache use from an independent application.
package caching

import (
	"context"
	"time"

	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Profile is an internal cache snapshot, not an automatically exported HTTP DTO.
type Profile struct {
	Email  mutatorqueries.DisplayEmail
	Labels []string
}
type Profiles = cache.Cache[model.ID[mutatorqueries.Member], Profile]

// ProfileEntries is declared once; repeated bindings reuse its identity.
var ProfileEntries = cache.Define("member-profiles-v1", cache.TextKeys[model.ID[mutatorqueries.Member]](), cache.JSON[Profile]())

func Bind(store *cache.Store) (Profiles, error) { return ProfileEntries.Bind(store) }

// SaveProfile explicitly selects the model's read accessor. The stored model and
// its ID remain untouched; consumers choose the representation to cache.
func SaveProfile(ctx context.Context, profiles Profiles, member mutatorqueries.Member) error {
	value, err := profileSnapshot(member)
	if err != nil {
		return err
	}
	return profiles.Put(ctx, member.ID, value, cache.For(5*time.Minute))
}
func profileSnapshot(member mutatorqueries.Member) (Profile, error) {
	email, err := member.AccessEmail()
	if err != nil {
		return Profile{}, err
	}
	return Profile{Email: email, Labels: []string{"active"}}, nil
}

// RememberProfile keeps application code focused on loading a concrete model and
// selecting its presentation fields. Foundry owns caching and local coalescing.
func RememberProfile(ctx context.Context, profiles Profiles, id model.ID[mutatorqueries.Member], load func(context.Context, model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error)) (Profile, error) {
	return profiles.Remember(ctx, id, cache.For(5*time.Minute), func(ctx context.Context) (Profile, error) {
		member, err := load(ctx, id)
		if err != nil {
			return Profile{}, err
		}
		return profileSnapshot(member)
	})
}

// FlexibleProfile serves a snapshot younger than a minute directly and an older
// one (up to ten more minutes) immediately while Foundry refreshes it in the
// background. The application only chooses its freshness policy and loader.
func FlexibleProfile(ctx context.Context, profiles Profiles, id model.ID[mutatorqueries.Member], load func(context.Context, model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error)) (Profile, error) {
	return profiles.Flexible(ctx, id, time.Minute, 10*time.Minute, func(ctx context.Context) (Profile, error) {
		member, err := load(ctx, id)
		if err != nil {
			return Profile{}, err
		}
		return profileSnapshot(member)
	})
}

func ReadProfile(ctx context.Context, profiles Profiles, id model.ID[mutatorqueries.Member]) (Profile, bool, error) {
	return profiles.Get(ctx, id)
}
