package redis

import (
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/redis/data"
)

func TestRedisTypedHashBatchReadsAndCounters(t *testing.T) {
	c, store, key := dataFixture(t)
	ctx := t.Context()
	declaration := data.DefineHash[string, string, int64]("records", 1, keyspace.StringKeys[string](), keyspace.StringKeys[string]())
	h, err := declaration.WithFieldDecoder(func(text string) (string, error) { return text, nil }).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	k := key(data.HashKind, "counters")
	if all, err := h.GetAll(ctx, "counters"); err != nil || len(all) != 0 {
		t.Fatal("missing hash", all, err)
	}
	if got, err := data.IncrementField(ctx, h, "counters", "views", 5); err != nil || got != 5 {
		t.Fatal(got, err)
	}
	if got, err := data.IncrementField(ctx, h, "counters", "views", -7); err != nil || got != -2 {
		t.Fatal(got, err)
	}
	if _, err := h.Set(ctx, "counters", "likes", 9); err != nil {
		t.Fatal(err)
	}
	values, err := h.GetMany(ctx, "counters", "views", "absent", "likes")
	if err != nil || len(values) != 3 {
		t.Fatal(values, err)
	}
	for i, want := range []int64{-2, 0, 9} {
		got, found := values[i].Get()
		if found != (i != 1) || got != want {
			t.Fatal(i, got, found)
		}
	}
	all, err := h.GetAll(ctx, "counters")
	if err != nil || len(all) != 2 || all[0].Field != "likes" || all[0].Value != 9 || all[1].Field != "views" || all[1].Value != -2 {
		t.Fatal(all, err)
	}
	// Fields sort by bytes (uppercase before lowercase), never by server locale.
	key(data.HashKind, "mixed")
	for i, field := range []string{"a", "Z", "B"} {
		if _, err := h.Set(ctx, "mixed", field, int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	mixed, err := h.GetAll(ctx, "mixed")
	if err != nil || len(mixed) != 3 || mixed[0].Field != "B" || mixed[1].Field != "Z" || mixed[2].Field != "a" {
		t.Fatal("hash fields were not in byte order", mixed, err)
	}
	// Expiry survives increments; overflow and non-integer values never mutate.
	if _, err := h.Expire(ctx, "counters", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := data.IncrementField(ctx, h, "counters", "views", 1); err != nil {
		t.Fatal(err)
	}
	if ttl := c.raw.PTTL(ctx, k.String()).Val(); ttl <= 0 {
		t.Fatal("increment removed expiry", ttl)
	}
	if err := c.raw.HSet(ctx, k.String(), "big", strconv.FormatInt(math.MaxInt64, 10)).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := data.IncrementField(ctx, h, "counters", "big", 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("overflow", err)
	}
	if got := c.raw.HGet(ctx, k.String(), "big").Val(); got != strconv.FormatInt(math.MaxInt64, 10) {
		t.Fatal("overflow changed value", got)
	}
	// The hash is full (3 fields): growth is rejected, existing fields change.
	if _, err := data.IncrementField(ctx, h, "counters", "fourth", 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("growth past bound", err)
	}
	if err := c.raw.HSet(ctx, k.String(), "likes", "1.5").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := data.IncrementField(ctx, h, "counters", "likes", 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("non-integer", err)
	}
	if got := c.raw.HGet(ctx, k.String(), "likes").Val(); got != "1.5" {
		t.Fatal("non-integer mutated", got)
	}
	if _, err := h.GetAll(ctx, "counters"); err == nil {
		t.Fatal("noncanonical value accepted")
	}
	noDecoder, err := declaration.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noDecoder.GetAll(ctx, "counters"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := h.GetMany(ctx, "counters", "a", "b", "c", "d"); !errors.Is(err, fault.Invalid) {
		t.Fatal("field batch bound", err)
	}
}

func TestRedisTypedSortedSets(t *testing.T) {
	c, store, key := dataFixture(t)
	ctx := t.Context()
	z, err := data.DefineSortedSet[string, string]("records", 1, keyspace.StringKeys[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	k := key(data.SortedSetKind, "board")
	for _, item := range []data.Scored[string]{{Member: "b", Score: 2}, {Member: "a", Score: 2}, {Member: "c", Score: math.Inf(-1)}} {
		if added, err := z.Add(ctx, "board", item.Member, item.Score); err != nil || !added {
			t.Fatal(item, added, err)
		}
	}
	if added, err := z.Add(ctx, "board", "d", 1); added || !errors.Is(err, fault.Invalid) {
		t.Fatal("growth past bound", added, err)
	}
	if added, err := z.Add(ctx, "board", "b", 0.5); err != nil || added {
		t.Fatal("rescoring a full set", added, err)
	}
	got, err := z.Range(ctx, "board", data.First(3))
	if err != nil || len(got) != 3 || got[0].Member != "c" || !math.IsInf(got[0].Score, -1) || got[1].Member != "b" || got[2].Member != "a" {
		t.Fatal(got, err)
	}
	last, err := z.Range(ctx, "board", data.Last(2))
	if err != nil || len(last) != 2 || last[0].Member != "a" || last[1].Member != "b" {
		t.Fatal(last, err)
	}
	byScore, err := z.RangeByScore(ctx, "board", data.ScoreRange{Min: 0.5, Max: math.Inf(1), ExcludeMin: true}, data.First(3))
	if err != nil || len(byScore) != 1 || byScore[0].Member != "a" || byScore[0].Score != 2 {
		t.Fatal(byScore, err)
	}
	if count, err := z.CountByScore(ctx, "board", data.Between(0, 2)); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if score, found, err := z.Score(ctx, "board", "b"); err != nil || !found || score != 0.5 {
		t.Fatal(score, found, err)
	}
	if rank, found, err := z.Rank(ctx, "board", "a", data.Descending); err != nil || !found || rank != 0 {
		t.Fatal(rank, found, err)
	}
	if _, found, err := z.Rank(ctx, "board", "missing", data.Ascending); err != nil || found {
		t.Fatal(found, err)
	}
	if score, err := z.Increment(ctx, "board", "b", 0.25); err != nil || score != 0.75 {
		t.Fatal(score, err)
	}
	if _, err := z.Increment(ctx, "board", "c", math.Inf(1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("NaN result", err)
	}
	if _, err := z.Add(ctx, "board", "a", math.NaN()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := z.Range(ctx, "board", data.First(4)); !errors.Is(err, fault.Invalid) {
		t.Fatal("window bound", err)
	}
	for _, member := range []string{"a", "b", "c"} {
		if removed, err := z.Remove(ctx, "board", member); err != nil || !removed {
			t.Fatal(member, removed, err)
		}
	}
	if exists, err := z.Exists(ctx, "board"); err != nil || exists {
		t.Fatal("empty sorted set remained", exists, err)
	}
	if err := c.raw.Set(ctx, k.String(), "wrong-type", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := z.Add(ctx, "board", "a", 1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if got := c.raw.Get(ctx, k.String()).Val(); got != "wrong-type" {
		t.Fatal("wrong type mutated", got)
	}
}

func TestRedisTypedListsAreBounded(t *testing.T) {
	c, store, key := dataFixture(t)
	ctx := t.Context()
	l, err := data.DefineList[string, int]("records", 1, keyspace.StringKeys[string]()).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	k := key(data.ListKind, "queue")
	if _, found, err := l.PopFront(ctx, "queue"); err != nil || found {
		t.Fatal(found, err)
	}
	if n, err := l.Push(ctx, "queue", 1, 2); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := l.Push(ctx, "queue", 3, 4); n != 0 || !errors.Is(err, fault.Invalid) {
		t.Fatal("push past bound", n, err)
	}
	if n, err := l.PushFront(ctx, "queue", 0); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if got, err := l.Range(ctx, "queue", 0, -1); err != nil || len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatal(got, err)
	}
	if _, err := l.Expire(ctx, "queue", cache.For(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if value, found, err := l.PopBack(ctx, "queue"); err != nil || !found || value != 2 {
		t.Fatal(value, found, err)
	}
	if ttl := c.raw.PTTL(ctx, k.String()).Val(); ttl <= 0 {
		t.Fatal("pop removed expiry", ttl)
	}
	if err := l.Trim(ctx, "queue", 1, -1); err != nil {
		t.Fatal(err)
	}
	if count, err := l.Count(ctx, "queue"); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := c.raw.RPush(ctx, k.String(), "1.0").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Range(ctx, "queue", 0, -1); err == nil {
		t.Fatal("noncanonical element accepted")
	}
	if value, found, err := l.PopFront(ctx, "queue"); err != nil || !found || value != 1 {
		t.Fatal(value, found, err)
	}
	if err := c.raw.RPush(ctx, k.String(), string(make([]byte, 65))).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.PopBack(ctx, "queue"); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized element popped", err)
	}
	if n := c.raw.LLen(ctx, k.String()).Val(); n != 2 {
		t.Fatal("oversized element removed", n)
	}
}
