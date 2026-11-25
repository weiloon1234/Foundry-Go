package caching

import (
	"context"
	"foundry.test/consumer/mutatorqueries"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func redisProfiles(ctx context.Context, password string, ProfileEntries cache.Declaration[model.ID[mutatorqueries.Member], Profile]) (*foundation.App, Profiles, error) {
	// doc-example:begin redis-1
	var RedisConnection = foundation.NewKey[*redis.Client]("app.redis")
	config := redis.DefaultConfig()
	config.Host = "redis.example.test"
	config.User = "application"
	config.Password = secret.New(password)

	app, err := foundry.New().Register(
		redis.Module("app.redis", RedisConnection, config),
	).Build(ctx)
	if err != nil {
		return nil, Profiles{}, err
	}
	ready := false
	defer func() {
		if !ready {
			app.Shutdown(context.Background())
		}
	}()
	// Start when the assembled application is ready; normal kernel execution shares
	// the application's lifecycle. Module installs cleanup before its health probe.
	if err := app.Start(ctx); err != nil {
		return nil, Profiles{}, err
	}
	client, err := foundation.Resolve(app.Services(), RedisConnection)
	if err != nil {
		return nil, Profiles{}, err
	}
	store, err := cache.NewStore(client, cache.DefaultConfig(keyspace.Namespace{
		Application: "example", Environment: "production",
	}))
	if err != nil {
		return nil, Profiles{}, err
	}
	profiles, err := ProfileEntries.Bind(store)
	if err != nil {
		return nil, Profiles{}, err
	}
	ready = true
	// doc-example:end redis-1
	return app, profiles, nil
}

func taggedProfile(ctx context.Context, store *cache.Store, profiles Profiles, member mutatorqueries.Member, loadProfile func(context.Context) (Profile, error), MemberChanges cache.TagDeclaration[model.ID[mutatorqueries.Member]]) error {
	// doc-example:begin redis-2
	tags, err := MemberChanges.Bind(store)
	if err != nil {
		return err
	}
	view, err := profiles.WithTags(tags.For(member.ID))
	if err != nil {
		return err
	}
	profile, err := view.Remember(ctx, member.ID, cache.For(5*time.Minute), loadProfile)
	if err != nil {
		return err
	}
	// After the corresponding domain change has committed:
	err = tags.Invalidate(ctx, member.ID)
	if err != nil {
		return err
	}
	// doc-example:end redis-2
	_ = profile
	return err
}
