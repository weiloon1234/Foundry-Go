package rediscommands_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/rediscommands"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

type recording struct {
	raw.Backend
	requests []raw.Request
}

func (b *recording) ExecuteRawBatch(ctx context.Context, requests []raw.Request, mode raw.Mode, l raw.Limits) ([]raw.Reply, error) {
	b.requests = append([]raw.Request(nil), requests...)
	return raw.CaptureReplies(ctx, []any{"OK", int64(1), "1"}, l.Reply)
}
func (b *recording) ExecuteRaw(ctx context.Context, request raw.Request, l raw.Limits) (raw.Reply, error) {
	b.requests = []raw.Request{request}
	return raw.CaptureReply(ctx, int64(5), l.Reply)
}
func TestConsumerTypedPipelineAndScript(t *testing.T) {
	b := &recording{}
	store, err := raw.NewStore(b, raw.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "raw"}))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := rediscommands.Visits.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	got, err := rediscommands.ResetAndVisit(t.Context(), store, keys, id)
	if err != nil || got.Count != 1 || got.Stored != "1" || len(b.requests) != 3 {
		t.Fatal(got, err)
	}
	for _, r := range b.requests {
		if err := r.Validate(keyspace.Namespace{Application: "consumer", Environment: "raw"}, raw.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := rediscommands.VisitScript(t.Context(), store, keys, id, 5); err != nil || n != 5 || !b.requests[0].IsScript() {
		t.Fatal(n, err)
	}
}
func TestConsumerRawAssemblyDoesNotConnect(t *testing.T) {
	config := redis.DefaultConfig()
	config.Host = "127.0.0.1"
	config.TLS = redis.DisableTLS
	client, err := redis.Prepare(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(context.Background()) })
	store, err := raw.NewStore(client, raw.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "raw"}))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := rediscommands.Visits.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rediscommands.ResetAndVisit(t.Context(), store, keys, id); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if stats := client.Stats(); stats.Open != 0 || stats.Ready || stats.Operations != 0 {
		t.Fatal(stats)
	}
}
