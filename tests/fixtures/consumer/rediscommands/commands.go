// Package rediscommands verifies the explicit typed raw Redis boundary.
package rediscommands

import (
	"context"

	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/redisdata"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

type MemberKeys = raw.Keys[model.ID[mutatorqueries.Member]]
type GroupKeys = raw.Keys[model.ID[models.Group]]

var Visits = raw.DefineKeys[model.ID[mutatorqueries.Member]]("member-visits", 1, keyspace.TextKeys[model.ID[mutatorqueries.Member]]())
var Scoreboards = raw.DefineKeys[model.ID[models.Group]]("group-scores", 1, keyspace.TextKeys[model.ID[models.Group]]())

// Ranking is an explicit raw Redis projection. Applications own the meaning of
// alternating member/score strings; normal typed model queries do not use this API.
func Ranking(ctx context.Context, store *raw.Store, keys GroupKeys, id model.ID[models.Group]) ([]string, error) {
	key, err := keys.For(ctx, id)
	if err != nil {
		return nil, err
	}
	command := raw.NewCommand("ZREVRANGE", raw.DecodeArray(raw.DecodeString())).Key(key).Arg(raw.Int64(0)).Arg(raw.Int64(9)).Arg(raw.Text("WITHSCORES"))
	return command.Run(ctx, store)
}

type VisitSnapshot struct {
	Count  int64
	Stored string
}

func ResetAndVisit(ctx context.Context, store *raw.Store, keys MemberKeys, id model.ID[mutatorqueries.Member]) (VisitSnapshot, error) {
	key, err := keys.For(ctx, id)
	if err != nil {
		return VisitSnapshot{}, err
	}
	pipeline, err := raw.NewPipeline(raw.Transaction)
	if err != nil {
		return VisitSnapshot{}, err
	}
	reset := raw.NewCommand("SET", raw.DecodeString()).Key(key).Arg(raw.Int64(0))
	if err := raw.Ignore(pipeline, reset); err != nil {
		return VisitSnapshot{}, err
	}
	increment := raw.NewCommand("INCRBY", raw.DecodeInt64()).Key(key).Arg(raw.Int64(1))
	count, err := raw.Queue(pipeline, increment)
	if err != nil {
		return VisitSnapshot{}, err
	}
	stored, err := raw.Queue(pipeline, raw.NewCommand("GET", raw.DecodeString()).Key(key))
	if err != nil {
		return VisitSnapshot{}, err
	}
	if err := pipeline.Run(ctx, store); err != nil {
		return VisitSnapshot{}, err
	}
	number, err := count.Value()
	if err != nil {
		return VisitSnapshot{}, err
	}
	text, err := stored.Value()
	if err != nil {
		return VisitSnapshot{}, err
	}
	return VisitSnapshot{Count: number, Stored: text}, nil
}
func VisitScript(ctx context.Context, store *raw.Store, keys MemberKeys, id model.ID[mutatorqueries.Member], amount int64) (int64, error) {
	key, err := keys.For(ctx, id)
	if err != nil {
		return 0, err
	}
	script := raw.NewScript("return redis.call('INCRBY',KEYS[1],ARGV[1])", key, raw.DecodeInt64()).Arg(raw.Int64(amount))
	return script.Run(ctx, store)
}
func ProfileAdapterKey(ctx context.Context, profiles redisdata.Profiles, id model.ID[mutatorqueries.Member]) (raw.Key, error) {
	key, err := profiles.AdapterKey(ctx, id)
	if err != nil {
		return raw.Key{}, err
	}
	return raw.FromDataKey(key)
}
