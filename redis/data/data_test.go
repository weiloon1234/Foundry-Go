package data

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type fakeBackend struct {
	Backend
	HashBackend
	SetBackend
	calls   atomic.Int32
	members []string
	keys    []Key
}

func (b *fakeBackend) DataExists(context.Context, Key) (bool, error) {
	b.calls.Add(1)
	return true, nil
}
func (b *fakeBackend) DataDeleteMany(_ context.Context, keys []Key) (uint64, error) {
	b.calls.Add(1)
	b.keys = append([]Key(nil), keys...)
	return uint64(len(keys)), nil
}
func (b *fakeBackend) SetMembers(context.Context, Key, Limits) ([]string, error) {
	b.calls.Add(1)
	return b.members, nil
}
func testStore(t *testing.T, b Backend, edit func(*Config)) *Store {
	t.Helper()
	c := DefaultConfig(keyspace.Namespace{Application: "typed-data", Environment: "test"})
	if edit != nil {
		edit(&c)
	}
	s, err := NewStore(b, c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestDeclarationsAndPureTypedBinding(t *testing.T) {
	b := &fakeBackend{}
	s := testStore(t, b, func(c *Config) { c.MaxDeclarations = 1 })
	d := DefineSet[string, int]("members", 1, keyspace.StringKeys[string]())
	for range 2 {
		set, err := d.Bind(s)
		if err != nil || set.Name() != "members" || set.Version() != 1 {
			t.Fatal(set, err)
		}
	}
	if _, err := DefineSet[string, string]("members", 1, keyspace.StringKeys[string]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := DefineHash[string, string, string]("members", 1, keyspace.StringKeys[string](), keyspace.StringKeys[string]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := DefineSet[string, int]("members", 2, keyspace.StringKeys[string]()).Bind(s); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("binding performed I/O")
	}
	if _, err := (SetDeclaration[string, int]{}).Bind(s); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := d.Bind(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := (HashDeclaration[string, string, int]{}).Bind(s); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestTypedDataValidatesBeforeBackendAccess(t *testing.T) {
	b := &fakeBackend{}
	s := testStore(t, b, func(c *Config) { c.MaxBatchKeys = 3 })
	d := DefineSet[string, int]("members", 1, keyspace.StringKeys[string]())
	set, err := d.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		if _, err := set.Exists(ctx, "id"); err == nil {
			t.Fatal("invalid context")
		}
		if _, err := set.DeleteMany(ctx); err == nil {
			t.Fatal("invalid empty batch context")
		}
	}
	if _, err := set.Expire(t.Context(), "id", cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, keys := range [][]string{{"a", "b", "c", "d"}, {"a", ""}, {"a", "bad\n"}} {
		if n, err := set.DeleteMany(t.Context(), keys...); err == nil || n != 0 {
			t.Fatal(n, err)
		}
	}
	if n, err := set.DeleteMany(t.Context()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid call reached backend")
	}
	if n, err := set.DeleteMany(t.Context(), "b", "a", "a"); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if err := ValidateBatch(b.keys); err != nil {
		t.Fatal(err)
	}
	if _, err := (Set[string, int]{}).Add(t.Context(), "id", 1); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, _, err := (Hash[string, string, int]{}).Get(t.Context(), "id", "f"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
func TestTypedDataRejectsUnownedAndNoncanonicalReplies(t *testing.T) {
	b := &fakeBackend{}
	s := testStore(t, b, func(c *Config) { c.Limits = Limits{Entries: 2, FieldBytes: 8, ValueBytes: 16, ReplyBytes: 32} })
	set, err := DefineSet[string, map[string]int]("maps", 1, keyspace.StringKeys[string]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, members := range [][]string{{`{"x":1}`, `{"x":1}`}, {`{"x":2}`, `{"x":1}`}, {`{"x":1.0}`}, {`{"x":1,"x":2}`}, {`"wrong"`}, {`{"x":1}`, `{"x":2}`, `{"x":3}`}, {strings.Repeat("x", 17)}} {
		b.members = members
		if got, err := set.Members(t.Context(), "id"); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	b.members = []string{`{"x":1}`}
	got, err := set.Members(t.Context(), "id")
	if err != nil {
		t.Fatal(err)
	}
	got[0]["x"] = 3
	again, err := set.Members(t.Context(), "id")
	if err != nil || again[0]["x"] != 1 {
		t.Fatal(again, err)
	}
}
func TestTypedDataCallbackFailureAndActualOwnership(t *testing.T) {
	for _, fail := range []func(){func() { panic("private payload") }, runtime.Goexit} {
		b := &fakeBackend{}
		s := testStore(t, b, nil)
		set, err := DefineSet[string, int]("callback", 1, keyspace.NewCodec(func(string) (string, error) { fail(); return "id", nil })).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		if yes, err := set.Exists(t.Context(), "id"); yes || !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private payload") {
			t.Fatal(yes, err)
		}
		if b.calls.Load() != 0 {
			t.Fatal("failed callback reached adapter")
		}
	}
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{}
		s := testStore(t, b, func(c *Config) { c.Timeout = time.Second; c.MaxConcurrent = 1 })
		entered, release := make(chan struct{}), make(chan struct{})
		set, err := DefineSet[string, int]("callback", 1, keyspace.NewCodec(func(key string) (string, error) { close(entered); <-release; return key, nil })).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := set.Exists(t.Context(), "id"); done <- err }()
		<-entered
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatal("abandoned callback", err)
		default:
		}
		// A held slot makes the next operation queue briefly, then report overload.
		if _, err := set.Exists(t.Context(), "other"); !errors.Is(err, fault.Overloaded) {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if b.calls.Load() != 0 {
			t.Fatal("canceled encoding reached backend")
		}
	})
}
func TestDataAddressAndLimits(t *testing.T) {
	ns := keyspace.Namespace{Application: "data", Environment: "test"}
	a, err := NewKey(ns, "family", 1, HashKind, "id")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewKey(ns, "family", 1, SetKind, "id")
	version, _ := NewKey(ns, "family", 2, HashKind, "id")
	if a.String() == other.String() || a.String() == version.String() {
		t.Fatal("feature/version collision")
	}
	for _, keys := range [][]Key{{{}}, {a, a}, {other, a}, make([]Key, MaxBatchKeys+1)} {
		if err := ValidateBatch(keys); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	for _, l := range []Limits{{}, {Entries: MaxEntries + 1, FieldBytes: 1, ValueBytes: 1, ReplyBytes: MaxEntries + 1}, {Entries: 2, FieldBytes: 1, ValueBytes: 5, ReplyBytes: 9}, {Entries: 1, FieldBytes: 1, ValueBytes: 1, ReplyBytes: MaxReplyBytes + 1}} {
		if err := l.Validate(); err == nil {
			t.Fatal(l)
		}
	}
}

func TestDataStoreAndDeclarationConfigurationValidation(t *testing.T) {
	b := &fakeBackend{}
	ns := keyspace.Namespace{Application: "configuration", Environment: "test"}
	for _, edit := range []func(*Config){
		func(c *Config) { c.Namespace = keyspace.Namespace{} }, func(c *Config) { c.MaxDeclarations = 0 },
		func(c *Config) { c.MaxKeyBytes = 0 }, func(c *Config) { c.MaxKeyBytes = keyspace.MaxKeyBytes + 1 },
		func(c *Config) { c.MaxConcurrent = 0 }, func(c *Config) { c.MaxBatchKeys = 0 },
		func(c *Config) { c.MaxBatchKeys = MaxBatchKeys + 1 }, func(c *Config) { c.Timeout = 0 },
		func(c *Config) { c.Limits = Limits{} },
	} {
		c := DefaultConfig(ns)
		edit(&c)
		if _, err := NewStore(b, c); !errors.Is(err, fault.Invalid) {
			t.Fatal(c, err)
		}
	}
	if _, err := NewStore(nil, DefaultConfig(ns)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	s := testStore(t, b, nil)
	for _, d := range []SetDeclaration[string, int]{DefineSet[string, int]("bad name", 1, keyspace.StringKeys[string]()), DefineSet[string, int]("valid", 0, keyspace.StringKeys[string]()), DefineSet[string, int]("valid", 1, keyspace.Codec[string]{})} {
		if _, err := d.Bind(s); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := DefineHash[string, string, int]("valid", 1, keyspace.StringKeys[string](), keyspace.Codec[string]{}).Bind(s); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	foreign, _ := NewKey(keyspace.Namespace{Application: "foreign", Environment: "test"}, "values", 1, SetKind, "x")
	owned, _ := NewKey(ns, "values", 1, SetKind, "x")
	if err := ValidateBatch([]Key{owned, foreign}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
