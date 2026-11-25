package caching

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

// doc-example:begin caching-2
func entryLifetime(ctx context.Context, profiles Profiles, memberID model.ID[Member], memberA, memberB model.ID[Member]) error {
	exists, err := profiles.Exists(ctx, memberID)
	if err != nil {
		return err
	}
	changed, err := profiles.Expire(ctx, memberID, cache.For(time.Hour))
	if err != nil {
		return err
	}
	removed, err := profiles.ForgetMany(ctx, memberA, memberB, memberA)
	if err != nil {
		return err
	}
	_ = exists
	_ = changed
	_ = removed
	return err
}

// doc-example:end caching-2

// doc-example:begin caching-3
func rememberMember(ctx context.Context, profiles Profiles, memberID model.ID[Member], loadMember func(context.Context, model.ID[Member]) (Member, error)) error {
	profile, err := profiles.Remember(ctx, memberID, cache.For(5*time.Minute),
		func(ctx context.Context) (Profile, error) {
			member, err := loadMember(ctx, memberID)
			if err != nil {
				return Profile{}, err
			}
			email, err := member.AccessEmail()
			if err != nil {
				return Profile{}, err
			}
			return Profile{Email: email}, nil
		})
	if err != nil {
		return err
	}
	_ = profile
	return err
}

// doc-example:end caching-3

// doc-example:begin caching-4
func flexibleMember(ctx context.Context, profiles Profiles, memberID model.ID[Member], loadProfile func(context.Context) (Profile, error)) error {
	profile, err := profiles.Flexible(ctx, memberID, time.Minute, 10*time.Minute, loadProfile)
	if err != nil {
		return err
	}
	_ = profile
	return err
}

// doc-example:end caching-4

// doc-example:begin caching-5
func memoryStore() error {
	backend, err := memory.New(memory.DefaultConfig(), clock.System{})
	if err != nil {
		return err
	}
	defer backend.Close(context.Background())
	store, err := cache.NewStore(backend, cache.DefaultConfig(keyspace.Namespace{
		Application: "shop",
		Environment: "development",
	}))
	if err != nil {
		return err
	}
	_ = store
	return nil
}

// doc-example:end caching-5

// doc-example:begin caching-1
func incrementViews(ctx context.Context, store *cache.Store, memberID model.ID[Member]) error {
	counts, err := ProfileViews.Bind(store)
	if err != nil {
		return err
	}
	views, err := counts.Increment(ctx, memberID, 1, cache.For(24*time.Hour))
	if err != nil {
		return err
	}
	_ = views
	return err
}

// doc-example:end caching-1
