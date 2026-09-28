package redis

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis/data"
	"github.com/weiloon1234/Foundry-Go/value"
)

func dataFixture(t *testing.T) (*Client, *data.Store, func(data.Kind, string) data.Key) {
	t.Helper()
	client, namespace, track := integrationAddresses(t, nil)
	config := data.DefaultConfig(namespace)
	config.Limits = data.Limits{Entries: 3, FieldBytes: 32, ValueBytes: 64, ReplyBytes: 192}
	store, err := data.NewStore(client, config)
	if err != nil {
		t.Fatal(err)
	}
	key := func(kind data.Kind, logical string) data.Key {
		k, err := data.NewKey(namespace, "records", 1, kind, logical)
		if err != nil {
			t.Fatal(err)
		}
		track(k.String())
		return k
	}
	return client, store, key
}
func TestRedisTypedHashAndSetBehavior(t *testing.T) {
	c, store, key := dataFixture(t)
	ctx := t.Context()
	h, err := data.DefineHash[string, string, value.Nullable[string]]("records", 1, keyspace.StringKeys[string](), keyspace.StringKeys[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	// Hash/set same name/version cannot accidentally register different schemas.
	if _, err := data.DefineSet[string, string]("records", 1, keyspace.StringKeys[string]()).Bind(store); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	sc := data.DefaultConfig(key(data.SetKind, "set").Namespace())
	sc.Limits = data.Limits{Entries: 3, FieldBytes: 32, ValueBytes: 64, ReplyBytes: 192}
	ss, err := data.NewStore(c, sc)
	if err != nil {
		t.Fatal(err)
	}
	s, err := data.DefineSet[string, map[string]int]("records", 1, keyspace.StringKeys[string]()).Bind(ss)
	if err != nil {
		t.Fatal(err)
	}
	hashKey := key(data.HashKind, "hash")
	setKey := key(data.SetKind, "set")
	if _, hit, err := h.Get(ctx, "hash", "null"); err != nil || hit {
		t.Fatal(hit, err)
	}
	if added, err := h.Set(ctx, "hash", "null", value.Null[string]()); err != nil || !added {
		t.Fatal(added, err)
	}
	if got, hit, err := h.Get(ctx, "hash", "null"); err != nil || !hit || !got.IsNull() {
		t.Fatal(got, hit, err)
	}
	if added, err := h.Set(ctx, "hash", "null", value.Of("live")); err != nil || added {
		t.Fatal(added, err)
	}
	if _, err := h.Expire(ctx, "hash", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before := c.raw.PTTL(ctx, hashKey.String()).Val()
	for _, field := range []string{"second", "third"} {
		if _, err := h.Set(ctx, "hash", field, value.Of(field)); err != nil {
			t.Fatal(err)
		}
	}
	if added, err := h.Set(ctx, "hash", "fourth", value.Of("no")); added || !errors.Is(err, fault.Invalid) {
		t.Fatal(added, err)
	}
	if count, err := h.Count(ctx, "hash"); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if after := c.raw.PTTL(ctx, hashKey.String()).Val(); after <= 0 || after > before {
		t.Fatal("hash changed expiry", before, after)
	}
	for _, field := range []string{"null", "second", "third"} {
		if removed, err := h.DeleteField(ctx, "hash", field); err != nil || !removed {
			t.Fatal(removed, err)
		}
	}
	if found, err := h.Exists(ctx, "hash"); err != nil || found {
		t.Fatal(found, err)
	}
	original := map[string]int{"a": 1, "b": 2}
	if added, err := s.Add(ctx, "set", original); err != nil || !added {
		t.Fatal(added, err)
	}
	original["a"] = 99
	if added, err := s.Add(ctx, "set", map[string]int{"b": 2, "a": 1}); err != nil || added {
		t.Fatal("canonical duplicate", added, err)
	}
	if yes, err := s.Contains(ctx, "set", map[string]int{"a": 1, "b": 2}); err != nil || !yes {
		t.Fatal(yes, err)
	}
	got, err := s.Members(ctx, "set")
	if err != nil || len(got) != 1 || got[0]["a"] != 1 {
		t.Fatal(got, err)
	}
	got[0]["a"] = 100
	again, err := s.Members(ctx, "set")
	if err != nil || again[0]["a"] != 1 {
		t.Fatal("member aliases stored value", again, err)
	}
	if _, err := s.Expire(ctx, "set", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before = c.raw.PTTL(ctx, setKey.String()).Val()
	for i := range 2 {
		if _, err := s.Add(ctx, "set", map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	if added, err := s.Add(ctx, "set", map[string]int{"new": 1}); added || !errors.Is(err, fault.Invalid) {
		t.Fatal(added, err)
	}
	if after := c.raw.PTTL(ctx, setKey.String()).Val(); after <= 0 || after > before {
		t.Fatal("set changed expiry", before, after)
	}
	if count, err := s.Count(ctx, "set"); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	for range 2 {
		if yes, err := s.Expire(ctx, "set", cache.Forever()); err != nil || !yes {
			t.Fatal(yes, err)
		}
	}
	if ttl := c.raw.PTTL(ctx, setKey.String()).Val(); ttl != -1 {
		t.Fatal(ttl)
	}
	members, err := s.Members(ctx, "set")
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if yes, err := s.Remove(ctx, "set", member); err != nil || !yes {
			t.Fatal(yes, err)
		}
	}
	if yes, err := s.Exists(ctx, "set"); err != nil || yes {
		t.Fatal(yes, err)
	}
	empty, err := s.Members(ctx, "set")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	if yes, err := s.Expire(ctx, "set", cache.Forever()); err != nil || yes {
		t.Fatal(yes, err)
	}
}
func TestRedisDataRejectsCorruptionBeforeMutation(t *testing.T) {
	c, _, key := dataFixture(t)
	ctx := t.Context()
	l := data.Limits{Entries: 2, FieldBytes: 4, ValueBytes: 4, ReplyBytes: 8}
	h := key(data.HashKind, "hash")
	s := key(data.SetKind, "set")
	if err := c.raw.HSet(ctx, h.String(), "ok", "12345").Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.HashGet(ctx, h, "ok", l); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.HashDelete(ctx, h, "ok", l); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if n := c.raw.HLen(ctx, h.String()).Val(); n != 1 {
		t.Fatal("corrupt field deleted", n)
	}
	if err := c.raw.SAdd(ctx, s.String(), "12345").Err(); err != nil {
		t.Fatal(err)
	}
	if got, err := c.SetMembers(ctx, s, l); got != nil || !errors.Is(err, fault.Invalid) {
		t.Fatal(got, err)
	}
	if err := c.raw.SAdd(ctx, s.String(), "1", "2").Err(); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.SetRemove(ctx, s, "1", l); ok || !errors.Is(err, fault.Invalid) {
		t.Fatal(ok, err)
	}
	keys := []data.Key{key(data.HashKind, "batch1"), key(data.HashKind, "batch2")}
	slices.SortFunc(keys, func(a, b data.Key) int { return strings.Compare(a.String(), b.String()) })
	if _, err := c.HashSet(ctx, keys[0], "f", "1", l); err != nil {
		t.Fatal(err)
	}
	if err := c.raw.Set(ctx, keys[1].String(), "wrong-type", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := c.DataDeleteMany(ctx, keys); n != 0 || !errors.Is(err, fault.Invalid) {
		t.Fatal(n, err)
	}
	if n := c.raw.Exists(ctx, keys[0].String()).Val(); n != 1 {
		t.Fatal("earlier entry deleted", n)
	}
	if _, err := c.DataExpire(ctx, keys[1], cache.Forever()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.HashSet(ctx, s, "f", "1", l); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := c.SetAdd(ctx, s, "12345", l); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestRedisDataConcurrentBoundsAndAtomicBatch(t *testing.T) {
	c, _, key := dataFixture(t)
	ctx := t.Context()
	l := data.Limits{Entries: 8, FieldBytes: 8, ValueBytes: 8, ReplyBytes: 64}
	k := key(data.SetKind, "concurrent")
	c2, err := Open(ctx, integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c2.Close(context.Background()) })
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := range 32 {
		wg.Go(func() {
			client := c
			if i%2 == 1 {
				client = c2
			}
			if yes, err := client.SetAdd(ctx, k, string(rune('a'+i)), l); err == nil && yes {
				wins.Add(1)
			} else if !errors.Is(err, fault.Invalid) {
				t.Error(yes, err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 8 {
		t.Fatal(wins.Load())
	}
	keys := []data.Key{key(data.SetKind, "batch-a"), key(data.HashKind, "batch-b")}
	slices.SortFunc(keys, func(a, b data.Key) int { return strings.Compare(a.String(), b.String()) })
	for _, k := range keys {
		var err error
		if k.Kind() == data.SetKind {
			_, err = c.SetAdd(ctx, k, "1", l)
		} else {
			_, err = c.HashSet(ctx, k, "f", "1", l)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	wins.Store(0)
	for range 16 {
		wg.Go(func() {
			n, err := c.DataDeleteMany(ctx, keys)
			if err != nil || n != 0 && n != 2 {
				t.Error(n, err)
			}
			if n == 2 {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
}
func TestRedisDataUnknownAcknowledgementNeverRetries(t *testing.T) {
	for _, op := range []string{"hash", "set", "expire", "batch"} {
		t.Run(op, func(t *testing.T) {
			c, _, key := dataFixture(t)
			ctx := t.Context()
			k := key(data.HashKind, "ack")
			if op == "set" {
				k = key(data.SetKind, "ack")
			}
			if op == "expire" || op == "batch" {
				if _, err := c.HashSet(ctx, k, "f", "1", data.DefaultLimits()); err != nil {
					t.Fatal(err)
				}
			}
			script := dataScript
			if op == "batch" {
				script = dataBatchScript
			}
			failure := errors.New("data acknowledgement lost")
			var calls atomic.Int32
			c.raw.AddHook(lostEntryAcknowledgement{script: script, calls: &calls, failure: failure})
			var yes bool
			var count uint64
			var err error
			switch op {
			case "hash":
				yes, err = c.HashSet(ctx, k, "f", "1", data.DefaultLimits())
			case "set":
				yes, err = c.SetAdd(ctx, k, "1", data.DefaultLimits())
			case "expire":
				yes, err = c.DataExpire(ctx, k, cache.For(time.Minute))
			case "batch":
				count, err = c.DataDeleteMany(ctx, []data.Key{k})
			}
			if yes || count != 0 || !errors.Is(err, failure) || calls.Load() != 1 {
				t.Fatal(yes, count, err, calls.Load())
			}
			if op == "batch" {
				if c.raw.Exists(ctx, k.String()).Val() != 0 {
					t.Fatal("batch not applied")
				}
			} else if c.raw.Exists(ctx, k.String()).Val() != 1 {
				t.Fatal("write not applied")
			}
			if op == "expire" && c.raw.PTTL(ctx, k.String()).Val() <= 0 {
				t.Fatal("expiry not applied")
			}
		})
	}
}
func TestRedisDataMalformedReplies(t *testing.T) {
	for _, reply := range []string{"+wrong\r\n", "*0\r\n", "*1\r\n:2\r\n", "*2\r\n:1\r\n:-1\r\n", "*2\r\n:1\r\n:2\r\n", "*2\r\n:0\r\n:0\r\n"} {
		t.Run(reply, func(t *testing.T) {
			config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
				if strings.EqualFold(args[0], "eval") {
					io.WriteString(conn, reply)
				} else {
					io.WriteString(conn, "+PONG\r\n")
				}
			})
			c := preparedTransport(t, config)
			if err := c.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			k, err := data.NewKey(keyspace.Namespace{Application: "data-reply", Environment: "test"}, "values", 1, data.SetKind, "one")
			if err != nil {
				t.Fatal(err)
			}
			if n, err := c.DataDeleteMany(t.Context(), []data.Key{k}); err == nil || n != 0 {
				t.Fatal(n, err)
			}
		})
	}
}

func TestRedisTypedDataRejectsStoredJSONAndHonorsFullBounds(t *testing.T) {
	c, store, key := dataFixture(t)
	ctx := t.Context()
	h, err := data.DefineHash[string, string, int]("records", 1, keyspace.StringKeys[string](), keyspace.StringKeys[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	k := key(data.HashKind, "json")
	for _, stored := range []string{`1.0`, `"wrong"`, `null`} {
		if err := c.raw.HSet(ctx, k.String(), "field", stored).Err(); err != nil {
			t.Fatal(err)
		}
		if got, hit, err := h.Get(ctx, "json", "field"); got != 0 || hit || err == nil {
			t.Fatal(got, hit, err)
		}
	}
	l := data.Limits{Entries: data.MaxEntries, FieldBytes: 32, ValueBytes: 32, ReplyBytes: data.MaxEntries * 32}
	setKey := key(data.SetKind, "maximum-members")
	members := make([]any, data.MaxEntries)
	for i := range members {
		members[i] = strconv.Itoa(i)
	}
	if err := c.raw.SAdd(ctx, setKey.String(), members...).Err(); err != nil {
		t.Fatal(err)
	}
	got, err := c.SetMembers(ctx, setKey, l)
	if err != nil || len(got) != data.MaxEntries || !slices.IsSorted(got) {
		t.Fatal(len(got), err)
	}
	if added, err := c.SetAdd(ctx, setKey, "new", l); added || !errors.Is(err, fault.Invalid) {
		t.Fatal(added, err)
	}
	if added, err := c.SetAdd(ctx, setKey, "0", l); added || err != nil {
		t.Fatal(added, err)
	}
	keys := make([]data.Key, data.MaxBatchKeys)
	for i := range keys {
		keys[i] = key(data.SetKind, "maximum-batch-"+strconv.Itoa(i))
		if _, err := c.SetAdd(ctx, keys[i], "1", l); err != nil {
			t.Fatal(err)
		}
	}
	slices.SortFunc(keys, func(a, b data.Key) int { return strings.Compare(a.String(), b.String()) })
	if n, err := c.DataDeleteMany(ctx, keys); err != nil || n != data.MaxBatchKeys {
		t.Fatal(n, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.DataDeleteMany(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n, err := c.DataDeleteMany(ctx, nil); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
