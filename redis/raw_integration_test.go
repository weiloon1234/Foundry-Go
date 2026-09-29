package redis

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis/data"
	"github.com/weiloon1234/Foundry-Go/redis/raw"
)

func rawFixture(t *testing.T) (*Client, *raw.Store, func(string) raw.Key) {
	t.Helper()
	client, ns, track := integrationAddresses(t, nil)
	store, err := raw.NewStore(client, raw.DefaultConfig(ns))
	if err != nil {
		t.Fatal(err)
	}
	key := func(logical string) raw.Key {
		k, err := raw.NewKey(ns, "resources", 1, logical)
		if err != nil {
			t.Fatal(err)
		}
		track(k.String())
		return k
	}
	return client, store, key
}
func TestRedisRawRustParityAndNativeEntryOperations(t *testing.T) {
	c, store, key := rawFixture(t)
	ctx := t.Context()
	leaderboard, counter, stream := key("leaderboard"), key("counter"), key("stream")
	for i, name := range []string{"alice", "bob"} {
		got, err := raw.NewCommand("ZADD", raw.DecodeInt64()).Key(leaderboard).Arg(raw.Int64(int64((i+1)*10))).Arg(raw.Text(name)).Run(ctx, store)
		if err != nil || got != 1 {
			t.Fatal(got, err)
		}
	}
	got, err := raw.NewCommand("ZREVRANGE", raw.DecodeArray(raw.DecodeString())).Key(leaderboard).Arg(raw.Int64(0)).Arg(raw.Int64(-1)).Arg(raw.Text("WITHSCORES")).Run(ctx, store)
	if err != nil || !slices.Equal(got, []string{"bob", "20", "alice", "10"}) {
		t.Fatal(got, err)
	}
	transaction, err := raw.NewPipeline(raw.Transaction)
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Ignore(transaction, raw.NewCommand("SET", raw.DecodeString()).Key(counter).Arg(raw.Int64(0))); err != nil {
		t.Fatal(err)
	}
	count, err := raw.Queue(transaction, raw.NewCommand("INCRBY", raw.DecodeInt64()).Key(counter).Arg(raw.Int64(2)))
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Run(ctx, store); err != nil {
		t.Fatal(err)
	}
	if n, err := count.Value(); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := raw.NewScript("return redis.call('INCRBY',KEYS[1],ARGV[1])", counter, raw.DecodeInt64()).Arg(raw.Int64(3)).Run(ctx, store); err != nil || n != 5 {
		t.Fatal(n, err)
	}
	if status, err := raw.NewCommand("XGROUP", raw.DecodeString()).Arg(raw.Text("CREATE")).Key(stream).Arg(raw.Text("workers")).Arg(raw.Text("0")).Arg(raw.Text("MKSTREAM")).Run(ctx, store); err != nil || status != "OK" {
		t.Fatal(status, err)
	}
	keys, err := raw.DefineKeys[string]("resources", 1, keyspace.StringKeys[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, logical := range []string{"leaderboard", "counter", "stream"} {
		if exists, err := keys.Exists(ctx, logical); err != nil || !exists {
			t.Fatal(exists, err)
		}
		if changed, err := keys.Expire(ctx, logical, cache.For(time.Minute)); err != nil || !changed {
			t.Fatal(changed, err)
		}
		ttl := c.raw.PTTL(ctx, key(logical).String()).Val()
		if ttl <= 0 || ttl > time.Minute {
			t.Fatal(ttl)
		}
		for range 2 {
			if changed, err := keys.Expire(ctx, logical, cache.Forever()); err != nil || !changed {
				t.Fatal(changed, err)
			}
		}
	}
	if n, err := keys.DeleteMany(ctx, "counter", "leaderboard", "stream", "counter"); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if yes, err := keys.Expire(ctx, "counter", cache.Forever()); err != nil || yes {
		t.Fatal(yes, err)
	}
	missing, err := raw.NewCommand("GET", raw.DecodeNullable(raw.DecodeString())).Key(counter).Run(ctx, store)
	if err != nil || !missing.IsNull() {
		t.Fatal(missing, err)
	}
}
func TestRedisRawExtendsTypedDataWithoutReconstructingItsKey(t *testing.T) {
	c, store, key := rawFixture(t)
	ctx := t.Context()
	ns := key("unused").Namespace()
	ds, err := data.NewStore(c, data.DefaultConfig(ns))
	if err != nil {
		t.Fatal(err)
	}
	h, err := data.DefineHash[string, string, int]("numbers", 2, keyspace.StringKeys[string](), keyspace.StringKeys[string]()).Bind(ds)
	if err != nil {
		t.Fatal(err)
	}
	address, err := h.AdapterKey(ctx, "member")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := h.Delete(ctx, "member"); err != nil {
			t.Error(err)
		}
	})
	if _, err := h.Set(ctx, "member", "one", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Set(ctx, "member", "two", 2); err != nil {
		t.Fatal(err)
	}
	exported, err := raw.FromDataKey(address)
	if err != nil {
		t.Fatal(err)
	}
	result, err := raw.NewCommand("HMGET", raw.DecodeArray(raw.DecodeNullable(raw.DecodeString()))).Key(exported).Arg(raw.Text("one")).Arg(raw.Text("two")).Arg(raw.Text("missing")).Run(ctx, store)
	if err != nil || len(result) != 3 || result[2].IsNull() == false {
		t.Fatal(result, err)
	}
	first, ok := result[0].Get()
	if !ok || first != "1" {
		t.Fatal(first, ok)
	}
}
func TestRedisRawPipelineServerErrorsDoNotClaimRollbackOrPublishResults(t *testing.T) {
	for _, mode := range []raw.Mode{raw.Pipelined, raw.Transaction} {
		t.Run(map[raw.Mode]string{raw.Pipelined: "pipeline", raw.Transaction: "transaction"}[mode], func(t *testing.T) {
			c, store, key := rawFixture(t)
			ctx := t.Context()
			first, last := key("first"), key("last")
			p, _ := raw.NewPipeline(mode)
			before, _ := raw.Queue(p, raw.NewCommand("SET", raw.DecodeString()).Key(first).Arg(raw.Text("retained")))
			if err := raw.Ignore(p, raw.NewCommand("HSET", raw.DecodeInt64()).Key(first).Arg(raw.Text("field")).Arg(raw.Text("wrong-type"))); err != nil {
				t.Fatal(err)
			}
			after, _ := raw.Queue(p, raw.NewCommand("SET", raw.DecodeString()).Key(last).Arg(raw.Text("also-retained")))
			if err := p.Run(ctx, store); err == nil {
				t.Fatal("server runtime error hidden")
			}
			for _, receipt := range []raw.Result[string]{before, after} {
				if got, err := receipt.Value(); got != "" || err == nil {
					t.Fatal(got, err)
				}
			}
			for _, k := range []raw.Key{first, last} {
				if yes := c.raw.Exists(ctx, k.String()).Val(); yes != 1 {
					t.Fatal("successful command incorrectly rolled back", yes)
				}
			}
		})
	}
}

type lostRawAcknowledgement struct {
	command  string
	pipeline bool
	calls    *atomic.Int32
	failure  error
}

func (h lostRawAcknowledgement) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h lostRawAcknowledgement) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, c driver.Cmder) error {
		err := next(ctx, c)
		if !h.pipeline && (strings.EqualFold(c.Name(), h.command) || h.command == "eval" && strings.EqualFold(c.Name(), "evalsha")) {
			h.calls.Add(1)
			if err == nil {
				return h.failure
			}
		}
		return err
	}
}
func (h lostRawAcknowledgement) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return func(ctx context.Context, c []driver.Cmder) error {
		err := next(ctx, c)
		if h.pipeline {
			h.calls.Add(1)
			if err == nil {
				return h.failure
			}
		}
		return err
	}
}
func TestRedisRawUnknownAcknowledgementHasOneAttempt(t *testing.T) {
	for _, mode := range []string{"command", "script", "pipeline", "transaction", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			c, store, key := rawFixture(t)
			ctx := t.Context()
			k := key("ack")
			failure := errors.New("acknowledgement lost")
			var calls atomic.Int32
			if mode == "expiry" {
				if err := c.raw.Set(ctx, k.String(), "1", time.Minute).Err(); err != nil {
					t.Fatal(err)
				}
			}
			hook := lostRawAcknowledgement{command: "incrby", calls: &calls, failure: failure}
			if mode == "script" || mode == "expiry" {
				hook.command = "eval"
			}
			hook.pipeline = mode == "pipeline" || mode == "transaction"
			c.raw.AddHook(hook)
			cmd := raw.NewCommand("INCRBY", raw.DecodeInt64()).Key(k).Arg(raw.Int64(1))
			var result int64
			var err error
			switch mode {
			case "command":
				result, err = cmd.Run(ctx, store)
			case "script":
				result, err = raw.NewScript("return redis.call('INCRBY',KEYS[1],1)", k, raw.DecodeInt64()).Run(ctx, store)
			case "expiry":
				var yes bool
				yes, err = c.ExpireRaw(ctx, k, cache.Forever())
				if yes {
					t.Fatal("uncertain expiry returned success")
				}
			default:
				kind := raw.Pipelined
				if mode == "transaction" {
					kind = raw.Transaction
				}
				p, _ := raw.NewPipeline(kind)
				receipt, queueErr := raw.Queue(p, cmd)
				if queueErr != nil {
					t.Fatal(queueErr)
				}
				err = p.Run(ctx, store)
				if value, valueErr := receipt.Value(); value != 0 || !errors.Is(valueErr, failure) {
					t.Fatal(value, valueErr)
				}
			}
			if result != 0 || !errors.Is(err, failure) || calls.Load() != 1 {
				t.Fatal(result, err, calls.Load())
			}
			if value := c.raw.Get(ctx, k.String()).Val(); value != "1" {
				t.Fatal("write retried or did not apply", value)
			}
			if mode == "expiry" && c.raw.PTTL(ctx, k.String()).Val() != -1 {
				t.Fatal("expiry did not apply")
			}
		})
	}
}
func TestRedisRawTransportRepliesAndServerErrorPrivacy(t *testing.T) {
	for _, reply := range []string{"$-1\r\n", "-ERR private-token\r\n", "*2\r\n$3\r\none\r\n$3\r\ntwo\r\n", "$8\r\ntoolarge\r\n"} {
		t.Run(reply, func(t *testing.T) {
			config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
				if strings.EqualFold(args[0], "get") {
					io.WriteString(conn, reply)
				} else {
					io.WriteString(conn, "+PONG\r\n")
				}
			})
			c := preparedTransport(t, config)
			if err := c.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			ns := keyspace.Namespace{Application: "raw-reply", Environment: "test"}
			cfg := raw.DefaultConfig(ns)
			cfg.Limits.Reply.Bytes = 4
			store, err := raw.NewStore(c, cfg)
			if err != nil {
				t.Fatal(err)
			}
			key, err := raw.NewKey(ns, "values", 1, "id")
			if err != nil {
				t.Fatal(err)
			}
			result, err := raw.NewCommand("GET", raw.DecodeNullable(raw.DecodeString())).Key(key).Run(t.Context(), store)
			if reply == "$-1\r\n" {
				if err != nil || !result.IsNull() {
					t.Fatal(result, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal(err)
			}
		})
	}
}
func TestRedisRawNilPipelineReplyDoesNotMaskALaterError(t *testing.T) {
	_, store, key := rawFixture(t)
	p, _ := raw.NewPipeline(raw.Pipelined)
	receipt, err := raw.Queue(p, raw.NewCommand("GET", raw.DecodeNullable(raw.DecodeString())).Key(key("missing")))
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Ignore(p, raw.NewCommand("INCRBY", raw.DecodeInt64()).Key(key("bad-integer")).Arg(raw.Text("not-an-integer"))); err != nil {
		t.Fatal(err)
	}
	if err := p.Run(t.Context(), store); err == nil {
		t.Fatal("nil first reply masked later error")
	}
	if _, err := receipt.Value(); err == nil {
		t.Fatal("failed batch published a nil result")
	}
}
func TestRedisRawNamespaceValidationPreservesOtherOwnedNamespace(t *testing.T) {
	c, store, key := rawFixture(t)
	ctx := t.Context()
	inside := key("inside")
	outside, err := raw.NewKey(keyspace.Namespace{Application: inside.Namespace().Application, Environment: inside.Namespace().Environment + "-other"}, "values", 1, "outside")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.raw.Del(context.Background(), outside.String()) })
	if err := c.raw.Set(ctx, outside.String(), "preserved", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.NewCommand("DEL", raw.DecodeInt64()).Key(inside).Key(outside).Run(ctx, store); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if got := c.raw.Get(ctx, outside.String()).Val(); got != "preserved" {
		t.Fatal(got)
	}
}
