package rediscommands

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func memberKey(ctx context.Context, client *redis.Client, namespace keyspace.Namespace, member Member) (raw.Key, error) {
	// doc-example:begin redis-commands-1
	store, err := raw.NewStore(client, raw.DefaultConfig(namespace))
	if err != nil {
		return raw.Key{}, err
	}
	keys, err := Visits.Bind(store)
	if err != nil {
		return raw.Key{}, err
	}
	key, err := keys.For(ctx, member.ID)
	if err != nil {
		return raw.Key{}, err
	}
	// doc-example:end redis-commands-1
	return key, nil
}

func rawRanking(ctx context.Context, store *raw.Store, scoreboardKey raw.Key) ([]string, error) {
	// doc-example:begin redis-commands-2
	command := raw.NewCommand("ZREVRANGE", raw.DecodeArray(raw.DecodeString())).
		Key(scoreboardKey).
		Arg(raw.Int64(0)).Arg(raw.Int64(9)).Arg(raw.Text("WITHSCORES"))
	return command.Run(ctx, store) // []string
	// doc-example:end redis-commands-2
}

func visitPipeline(ctx context.Context, store *raw.Store, key raw.Key) error {
	// doc-example:begin redis-commands-3
	pipeline, err := raw.NewPipeline(raw.Transaction)
	if err != nil {
		return err
	}
	err = raw.Ignore(pipeline,
		raw.NewCommand("SET", raw.DecodeString()).Key(key).Arg(raw.Int64(0)))
	if err != nil {
		return err
	}
	count, err := raw.Queue(pipeline,
		raw.NewCommand("INCRBY", raw.DecodeInt64()).Key(key).Arg(raw.Int64(1)))
	if err != nil {
		return err
	}
	stored, err := raw.Queue(pipeline,
		raw.NewCommand("GET", raw.DecodeString()).Key(key))
	if err != nil {
		return err
	}
	err = pipeline.Run(ctx, store)
	if err != nil {
		return err
	}
	number, err := count.Value() // int64
	if err != nil {
		return err
	}
	text, err := stored.Value() // string
	if err != nil {
		return err
	}
	// doc-example:end redis-commands-3
	_ = number
	_ = text
	return err
}

func incrementScript(ctx context.Context, store *raw.Store, key raw.Key) (int64, error) {
	// doc-example:begin redis-commands-4
	script := raw.NewScript(
		"return redis.call('INCRBY',KEYS[1],ARGV[1])",
		key, raw.DecodeInt64(),
	).Arg(raw.Int64(3))
	return script.Run(ctx, store)
	// doc-example:end redis-commands-4
}
