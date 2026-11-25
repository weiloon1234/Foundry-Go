package caching

import (
	"context"
	"time"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// doc-example:begin redis-1
func redisProfiles(ctx context.Context, password string, ProfileEntries cache.Declaration[model.ID[Member], Profile]) error {
	var RedisConnection = foundation.NewKey[*redis.Client]("app.redis")
	config := redis.DefaultConfig()
	config.Host = "redis.example.test"
	config.User = "application"
	config.Password = secret.New(password)

	app, err := foundry.New().Register(
		redis.Module("app.redis", RedisConnection, config),
	).Build(ctx)
	if err != nil {
		return err
	}
	defer app.Shutdown(context.Background())
	// Start when the assembled application is ready; normal kernel execution shares
	// the application's lifecycle. Module installs cleanup before its health probe.
	if err := app.Start(ctx); err != nil {
		return err
	}
	client, err := foundation.Resolve(app.Services(), RedisConnection)
	if err != nil {
		return err
	}
	store, err := cache.NewStore(client, cache.DefaultConfig(keyspace.Namespace{
		Application: "example", Environment: "production",
	}))
	if err != nil {
		return err
	}
	profiles, err := ProfileEntries.Bind(store)
	if err != nil {
		return err
	}
	_ = profiles
	return err
}

// doc-example:end redis-1

// doc-example:begin redis-2
func taggedProfile(ctx context.Context, store *cache.Store, profiles Profiles, member Member, loadProfile func(context.Context) (Profile, error), MemberChanges cache.TagDeclaration[model.ID[Member]]) error {
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
	_ = profile
	return err
}

// doc-example:end redis-2
