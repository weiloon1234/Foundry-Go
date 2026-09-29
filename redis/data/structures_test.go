package data

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/value"
)

// structureBackend records calls and replays configured adapter replies.
type structureBackend struct {
	fakeBackend
	calls   atomic.Int32
	fields  []StoredField
	values  []value.Optional[string]
	scored  []StoredMember
	texts   []string
	counter int64
}

func (b *structureBackend) HashGetMany(context.Context, Key, []string, Limits) ([]value.Optional[string], error) {
	b.calls.Add(1)
	return b.values, nil
}
func (b *structureBackend) HashGetAll(context.Context, Key, Limits) ([]StoredField, error) {
	b.calls.Add(1)
	return b.fields, nil
}
func (b *structureBackend) HashIncrement(_ context.Context, _ Key, _ string, delta int64, _ Limits) (int64, error) {
	b.calls.Add(1)
	b.counter += delta
	return b.counter, nil
}
func (b *structureBackend) SortedSetAdd(context.Context, Key, string, float64, Limits) (bool, error) {
	b.calls.Add(1)
	return true, nil
}
func (b *structureBackend) SortedSetIncrement(context.Context, Key, string, float64, Limits) (float64, error) {
	b.calls.Add(1)
	return 1, nil
}
func (b *structureBackend) SortedSetRemove(context.Context, Key, string, Limits) (bool, error) {
	b.calls.Add(1)
	return true, nil
}
func (b *structureBackend) SortedSetScore(context.Context, Key, string, Limits) (float64, bool, error) {
	b.calls.Add(1)
	return 1, true, nil
}
func (b *structureBackend) SortedSetRank(context.Context, Key, string, Order, Limits) (uint64, bool, error) {
	b.calls.Add(1)
	return 0, true, nil
}
func (b *structureBackend) SortedSetRange(context.Context, Key, Window, Limits) ([]StoredMember, error) {
	b.calls.Add(1)
	return b.scored, nil
}
func (b *structureBackend) SortedSetRangeByScore(context.Context, Key, ScoreRange, Window, Limits) ([]StoredMember, error) {
	b.calls.Add(1)
	return b.scored, nil
}
func (b *structureBackend) SortedSetCountByScore(context.Context, Key, ScoreRange, Limits) (uint64, error) {
	b.calls.Add(1)
	return 0, nil
}
func (b *structureBackend) ListPush(_ context.Context, _ Key, values []string, _ End, _ Limits) (uint64, error) {
	b.calls.Add(1)
	return uint64(len(values)), nil
}
func (b *structureBackend) ListPop(context.Context, Key, End, Limits) (string, bool, error) {
	b.calls.Add(1)
	if len(b.texts) == 0 {
		return "", false, nil
	}
	return b.texts[0], true, nil
}
func (b *structureBackend) ListRange(context.Context, Key, int64, int64, Limits) ([]string, error) {
	b.calls.Add(1)
	return b.texts, nil
}
func (b *structureBackend) ListTrim(context.Context, Key, int64, int64, Limits) error {
	b.calls.Add(1)
	return nil
}

func TestHashBatchReadsAndCountersKeepTypes(t *testing.T) {
	b := &structureBackend{}
	s := testStore(t, b, func(c *Config) { c.Limits = Limits{Entries: 2, FieldBytes: 8, ValueBytes: 16, ReplyBytes: 32} })
	declaration := DefineHash[string, string, int64]("counts", 1, keyspace.StringKeys[string](), keyspace.StringKeys[string]())
	plain, err := declaration.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.GetAll(t.Context(), "id"); !errors.Is(err, fault.Invalid) {
		t.Fatal("GetAll without a field decoder", err)
	}
	if _, err := plain.GetMany(t.Context(), "id"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := plain.GetMany(t.Context(), "id", "a", "b", "c"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid batch reached the adapter")
	}
	b.values = []value.Optional[string]{value.Set("7"), {}}
	got, err := plain.GetMany(t.Context(), "id", "a", "b")
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if first, ok := got[0].Get(); !ok || first != 7 || got[1].IsSet() {
		t.Fatal(got)
	}
	b.values = []value.Optional[string]{value.Set("7")}
	if _, err := plain.GetMany(t.Context(), "id", "a", "b"); err == nil {
		t.Fatal("short adapter reply accepted")
	}
	decoded, err := declaration.WithFieldDecoder(func(text string) (string, error) { return strings.TrimSuffix(text, "!"), nil }).Bind(s)
	if err != nil {
		t.Fatal("same declaration identity must rebind", err)
	}
	b.fields = []StoredField{{Field: "a", Value: "1"}, {Field: "b", Value: "2"}}
	all, err := decoded.GetAll(t.Context(), "id")
	if err != nil || len(all) != 2 || all[1].Field != "b" || all[1].Value != 2 {
		t.Fatal(all, err)
	}
	for _, fields := range [][]StoredField{{{Field: "b", Value: "1"}, {Field: "a", Value: "2"}}, {{Field: "a!", Value: "1"}}, {{Field: "a", Value: "1.0"}}} {
		b.fields = fields
		if got, err := decoded.GetAll(t.Context(), "id"); err == nil || got != nil {
			t.Fatal("unordered, non-inverting or noncanonical reply accepted", fields, got)
		}
	}
	if total, err := IncrementField(t.Context(), plain, "id", "a", -3); err != nil || total != -3 {
		t.Fatal(total, err)
	}
}

func TestSortedSetsValidateScoresWindowsAndOrder(t *testing.T) {
	b := &structureBackend{}
	s := testStore(t, b, func(c *Config) { c.Limits = Limits{Entries: 2, FieldBytes: 8, ValueBytes: 16, ReplyBytes: 32} })
	z, err := DefineSortedSet[string, string]("board", 1, keyspace.StringKeys[string]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := z.Add(t.Context(), "id", "m", math.NaN()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, window := range []Window{{}, {Count: 3}, {Count: 1, Offset: -1}, {Count: 1, Order: 9}} {
		if _, err := z.Range(t.Context(), "id", window); !errors.Is(err, fault.Invalid) {
			t.Fatal(window, err)
		}
	}
	for _, scores := range []ScoreRange{{Min: 2, Max: 1}, {Min: math.NaN(), Max: 1}} {
		if _, err := z.RangeByScore(t.Context(), "id", scores, First(1)); !errors.Is(err, fault.Invalid) {
			t.Fatal(scores, err)
		}
		if _, err := z.CountByScore(t.Context(), "id", scores); !errors.Is(err, fault.Invalid) {
			t.Fatal(scores, err)
		}
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid sorted set call reached the adapter")
	}
	b.scored = []StoredMember{{Member: `"a"`, Score: 1}, {Member: `"b"`, Score: 1}}
	got, err := z.Range(t.Context(), "id", First(2))
	if err != nil || len(got) != 2 || got[0].Member != "a" || got[1].Score != 1 {
		t.Fatal(got, err)
	}
	for _, scored := range [][]StoredMember{{{Member: `"b"`, Score: 1}, {Member: `"a"`, Score: 1}}, {{Member: `"a"`, Score: 2}, {Member: `"b"`, Score: 1}}, {{Member: `"a"`, Score: math.NaN()}}, {{Member: `"a"`}, {Member: `"b"`}, {Member: `"c"`}}} {
		b.scored = scored
		if got, err := z.Range(t.Context(), "id", First(2)); err == nil || got != nil {
			t.Fatal("misordered or oversized reply accepted", scored, got)
		}
	}
	b.scored = []StoredMember{{Member: `"b"`, Score: 2}, {Member: `"a"`, Score: 2}}
	if got, err := z.Range(t.Context(), "id", Last(2)); err != nil || got[0].Member != "b" {
		t.Fatal("descending order rejected", got, err)
	}
}

func TestListsBoundPushesAndDecodeElements(t *testing.T) {
	b := &structureBackend{}
	s := testStore(t, b, func(c *Config) { c.Limits = Limits{Entries: 2, FieldBytes: 8, ValueBytes: 16, ReplyBytes: 32} })
	l, err := DefineList[string, int]("queue", 1, keyspace.StringKeys[string]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Push(t.Context(), "id"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := l.Push(t.Context(), "id", 1, 2, 3); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := l.Range(t.Context(), "id", 0, MaxEntries+1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid list call reached the adapter")
	}
	if n, err := l.PushFront(t.Context(), "id", 1, 2); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	b.texts = []string{"4", "5"}
	if got, err := l.Range(t.Context(), "id", 0, -1); err != nil || len(got) != 2 || got[1] != 5 {
		t.Fatal(got, err)
	}
	if got, found, err := l.PopFront(t.Context(), "id"); err != nil || !found || got != 4 {
		t.Fatal(got, found, err)
	}
	b.texts = []string{"4.0"}
	if _, found, err := l.PopBack(t.Context(), "id"); err == nil || found {
		t.Fatal("noncanonical element accepted", found, err)
	}
	if _, err := DefineList[string, int]("unsupported", 1, keyspace.StringKeys[string]()).Bind(testStore(t, &fakeBackend{}, nil)); !errors.Is(err, fault.Invalid) {
		t.Fatal("list bound without adapter support", err)
	}
	if _, err := DefineSortedSet[string, int]("queue", 1, keyspace.StringKeys[string]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal("kinds share name/version ownership", err)
	}
}

func TestStoreAdmissionQueuesBeforeOverload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{}
		s := testStore(t, b, func(c *Config) { c.Timeout = time.Second; c.MaxConcurrent = 1 })
		entered, release := make(chan struct{}), make(chan struct{})
		first := true
		set, err := DefineSet[string, int]("queued", 1, keyspace.NewCodec(func(key string) (string, error) {
			if first {
				first = false
				close(entered)
				<-release
			}
			return key, nil
		})).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := set.Exists(t.Context(), "held"); done <- err }()
		<-entered
		queued := make(chan error, 1)
		go func() { _, err := set.Exists(t.Context(), "waiting"); queued <- err }()
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := <-queued; err != nil {
			t.Fatal("queued operation failed instead of waiting", err)
		}
	})
}
