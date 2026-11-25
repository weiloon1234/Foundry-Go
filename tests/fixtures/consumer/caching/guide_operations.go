package caching

import (
	"context"
	"errors"
	"foundry.test/consumer/mutatorqueries"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
)

func entryLifetime(ctx context.Context, profiles Profiles, memberID model.ID[mutatorqueries.Member], memberA, memberB model.ID[mutatorqueries.Member]) error {
	// doc-example:begin caching-2
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
	// doc-example:end caching-2
	_ = exists
	_ = changed
	_ = removed
	return err
}

func rememberMember(ctx context.Context, profiles Profiles, memberID model.ID[mutatorqueries.Member], loadMember func(context.Context, model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error)) (Profile, error) {
	// doc-example:begin caching-3
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
		return Profile{}, err
	}
	// doc-example:end caching-3
	return profile, nil
}

func flexibleMember(ctx context.Context, profiles Profiles, memberID model.ID[mutatorqueries.Member], loadProfile func(context.Context) (Profile, error)) (Profile, error) {
	// doc-example:begin caching-4
	profile, err := profiles.Flexible(ctx, memberID, time.Minute, 10*time.Minute, loadProfile)
	if err != nil {
		return Profile{}, err
	}
	// doc-example:end caching-4
	return profile, nil
}

func memoryStore() (*cache.Store, *memory.Backend, error) {
	// doc-example:begin caching-5
	backend, err := memory.New(memory.DefaultConfig(), clock.System{})
	if err != nil {
		return nil, nil, err
	}
	store, err := cache.NewStore(backend, cache.DefaultConfig(keyspace.Namespace{
		Application: "shop",
		Environment: "development",
	}))
	if err != nil {
		return nil, nil, errors.Join(err, backend.Close(context.Background()))
	}
	// doc-example:end caching-5
	return store, backend, nil
}

func incrementViews(ctx context.Context, store *cache.Store, memberID model.ID[mutatorqueries.Member]) (int64, error) {
	// doc-example:begin caching-1
	counts, err := ProfileViews.Bind(store)
	if err != nil {
		return 0, err
	}
	views, err := counts.Increment(ctx, memberID, 1, cache.For(24*time.Hour))
	if err != nil {
		return 0, err
	}
	// doc-example:end caching-1
	return views, nil
}
