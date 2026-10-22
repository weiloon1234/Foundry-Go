package application

import (
	"context"
	"encoding/json"
	"flag"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

// WithRedisStores registers typed application stores with the configured plan.
func WithRedisStores(stores ...infrastructure.RedisStore) Option {
	option := infrastructure.WithRedisStores(stores...)
	return func(o *options) error { o.infrastructure = append(o.infrastructure, option); return nil }
}

// RedisKeys inspects the builder's configured Redis consumers without Build,
// opening pools, reading credentials, or invoking application callbacks. It uses
// the same infrastructure options and resolution as Build. External providers
// are opaque; use WithRedisStores for application rate-limit/lockout consumers.
func (b *Builder) RedisKeys() ([]infrastructure.RedisConnectionInventory, error) {
	if b == nil || b.state == nil {
		return nil, fault.New(fault.Invalid, "Redis inventory requires an application builder")
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.err != nil {
		return nil, b.state.err
	}
	plan, err := infrastructure.Configure(b.state.settings.Services, b.state.options.infrastructure...)
	if err != nil {
		return nil, err
	}
	return plan.RedisKeys(), nil
}

type redisKeysArguments struct{ JSON bool }

// RedisKeysCommand snapshots the configured inventory for a service-free CLI
// registry, following AboutCommand. Use the name "redis.keys" by convention.
// Register this command before Build and execute it without starting services.
func RedisKeysCommand(name cli.Name, builder *Builder) (cli.Declaration, error) {
	inventory, err := builder.RedisKeys()
	if err != nil {
		return cli.Declaration{}, err
	}
	command := cli.Define(name, "Describe exact Redis key and channel prefixes", cli.Flags(func(flags *flag.FlagSet, args *redisKeysArguments) {
		flags.BoolVar(&args.JSON, "json", false, "write JSON instead of a table")
	}, nil))
	return command.Declare(func(foundation.Resolver) (cli.Handler[redisKeysArguments], error) {
		return func(ctx context.Context, args redisKeysArguments, streams cli.Streams) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if args.JSON {
				encoder := json.NewEncoder(streams.Out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(inventory)
			}
			rows := [][]string{}
			for _, connection := range inventory {
				for _, key := range connection.Keys {
					rows = append(rows, []string{string(connection.Connection), "key", key.Prefix})
				}
				for _, channel := range connection.Channels {
					rows = append(rows, []string{string(connection.Connection), "channel", channel.Prefix})
				}
			}
			return cli.WriteTable(streams.Out, []string{"Connection", "Kind", "Prefix"}, rows)
		}, nil
	})
}
