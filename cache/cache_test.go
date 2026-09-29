package cache_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/cache/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type userKey string
type profile struct {
	Name       string
	Scores     []int
	Attributes map[string]any
}

var profiles = cache.Define("profiles", cache.StringKeys[userKey](), cache.JSON[profile]())
var namespace = cache.Namespace{Application: "foundry-cache", Environment: "test"}

func store(t *testing.T, configure func(*cache.Config)) (*cache.Store, *memory.Backend, *testkit.Clock) {
	t.Helper()
	clock := testkit.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	backend, err := memory.New(memory.DefaultConfig(), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	config := cache.DefaultConfig(namespace)
	if configure != nil {
		configure(&config)
	}
	value, err := cache.NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	return value, backend, clock
}
func TestTypedSnapshotsMissZeroExpiryAndForget(t *testing.T) {
	s, _, clock := store(t, nil)
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := c.Get(t.Context(), "42"); err != nil || found || !reflect.DeepEqual(got, profile{}) {
		t.Fatal("miss semantics", got, found, err)
	}
	input := profile{Name: "member", Scores: []int{1, 2}, Attributes: map[string]any{"large": json.Number("9007199254740993")}}
	if err := c.Put(t.Context(), "42", input, cache.For(time.Second)); err != nil {
		t.Fatal(err)
	}
	input.Scores[0] = 99
	input.Attributes["large"] = "changed"
	got, found, err := c.Get(t.Context(), "42")
	if err != nil || !found || got.Scores[0] != 1 || got.Attributes["large"] != json.Number("9007199254740993") {
		t.Fatal("snapshot or number lost", got, found, err)
	}
	got.Scores[0] = 88
	again, _, err := c.Get(t.Context(), "42")
	if err != nil || again.Scores[0] != 1 {
		t.Fatal("read aliased cache data")
	}
	clock.Advance(time.Second)
	if _, found, err := c.Get(t.Context(), "42"); err != nil || found {
		t.Fatal("exact expiry boundary", found, err)
	}
	if err := c.Put(t.Context(), "42", profile{}, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(24 * time.Hour)
	if got, found, err := c.Get(t.Context(), "42"); err != nil || !found || !reflect.DeepEqual(got, profile{}) {
		t.Fatal("zero value became a miss", found, err)
	}
	if removed, err := c.Forget(t.Context(), "42"); err != nil || !removed {
		t.Fatal("forget", removed, err)
	}
	if removed, err := c.Forget(t.Context(), "42"); err != nil || removed {
		t.Fatal("forget absent", removed, err)
	}
	pointers, err := cache.Define("pointers", cache.StringKeys[string](), cache.JSON[*profile]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := pointers.Put(t.Context(), "nil", nil, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if got, found, err := pointers.Get(t.Context(), "nil"); err != nil || !found || got != nil {
		t.Fatal("cached nil confused with absent", found, err)
	}
}
func TestDeclarationsAreValidatedBoundedAndNotInterchangeable(t *testing.T) {
	s, _, _ := store(t, func(c *cache.Config) { c.MaxDeclarations = 1 })
	if _, err := profiles.Bind(s); err != nil {
		t.Fatal(err)
	}
	copy := profiles
	if _, err := copy.Bind(s); err != nil {
		t.Fatal("same declaration cannot rebind", err)
	}
	other := cache.Define("profiles", cache.StringKeys[userKey](), cache.JSON[int]())
	if _, err := other.Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal("conflicting payload bound", err)
	}
	if _, err := cache.Define("profiles", cache.StringKeys[userKey](), cache.JSON[profile]()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal("separate codecs could share a family", err)
	}
	if _, err := cache.Define("another", cache.StringKeys[userKey](), cache.JSON[profile]()).Bind(s); !errors.Is(err, fault.Invalid) {
		t.Fatal("declaration limit ignored", err)
	}
	for _, d := range []cache.Declaration[string, int]{{}, cache.Define("bad:name", cache.StringKeys[string](), cache.JSON[int]()), cache.Define("keys", cache.KeyCodec[string]{}, cache.JSON[int]()), cache.Define("values", cache.StringKeys[string](), cache.Codec[int]{})} {
		if _, err := d.Bind(s); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid declaration accepted", err)
		}
	}
	if _, err := profiles.Bind(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil store accepted")
	}
	var zero cache.Cache[string, int]
	if _, _, err := zero.Get(t.Context(), "key"); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero handle accepted", err)
	}
}

type readBackend struct {
	cache.Backend
	read func(context.Context, cache.EntryKey) ([]byte, bool, error)
}

func (b readBackend) Get(ctx context.Context, key cache.EntryKey) ([]byte, bool, error) {
	return b.read(ctx, key)
}
func TestBackendErrorsAreNotMissesOrExposedSecrets(t *testing.T) {
	cause := errors.New("private-driver-credential")
	backend := readBackend{read: func(context.Context, cache.EntryKey) ([]byte, bool, error) { return []byte("partial"), true, cause }}
	s, err := cache.NewStore(backend, cache.DefaultConfig(namespace))
	if err != nil {
		t.Fatal(err)
	}
	c, err := profiles.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := c.Get(t.Context(), "42")
	if !errors.Is(err, cause) || found || !reflect.DeepEqual(got, profile{}) {
		t.Fatal("backend error became result", got, found, err)
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, err), cause.Error()) {
			t.Fatal("backend cause leaked through formatting")
		}
	}
}
func TestCodecFailureNeverPublishesOrWrites(t *testing.T) {
	for _, phase := range []string{"key", "encode", "decode"} {
		for _, mode := range []string{"error", "panic", "goexit"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				s, backend, _ := store(t, nil)
				cause := errors.New("private-codec-value")
				fail := func() error {
					switch mode {
					case "panic":
						panic(cause)
					case "goexit":
						runtime.Goexit()
					}
					return cause
				}
				keys := cache.NewKeyCodec(func(k string) (string, error) {
					if phase == "key" {
						return k, fail()
					}
					return k, nil
				})
				codec := cache.NewCodec(func(v int) ([]byte, error) {
					if phase == "encode" {
						return []byte("partial"), fail()
					}
					return []byte("7"), nil
				}, func([]byte) (int, error) { return 42, fail() })
				c, err := cache.Define("failures", keys, codec).Bind(s)
				if err != nil {
					t.Fatal(err)
				}
				if phase == "decode" {
					if err := c.Put(t.Context(), "key", 7, cache.Forever()); err != nil {
						t.Fatal(err)
					}
					var value int
					var found bool
					value, found, err = c.Get(t.Context(), "key")
					if value != 0 || found {
						t.Fatal("failed decode published partial value")
					}
				} else {
					err = c.Put(t.Context(), "key", 7, cache.Forever())
					expectedMetadata := 0
					if phase == "encode" {
						expectedMetadata = 1
					}
					if backend.Stats().Entries != expectedMetadata {
						t.Fatal("failed codec wrote payload data")
					}
				}
				if mode == "error" && !errors.Is(err, cause) || mode != "error" && !errors.Is(err, fault.Panicked) {
					t.Fatal("codec failure identity lost", err)
				}
				if strings.Contains(fmt.Sprintf("%+v", err), cause.Error()) {
					t.Fatal("codec payload leaked")
				}
			})
		}
	}
}
func TestOperationLimitsAndCancellationBeforePublication(t *testing.T) {
	s, _, _ := store(t, func(c *cache.Config) { c.MaxKeyBytes = 3; c.MaxValueBytes = 3 })
	c, err := cache.Define("small", cache.StringKeys[string](), cache.JSON[int]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "key", 1, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		key   string
		value int
		ttl   cache.TTL
	}{{"long", 1, cache.Forever()}, {"key", 1234, cache.Forever()}, {"key", 2, cache.TTL{}}, {"key", 2, cache.For(-1)}} {
		if err := c.Put(t.Context(), test.key, test.value, test.ttl); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid write accepted", err)
		}
	}
	if value, found, err := c.Get(t.Context(), "key"); err != nil || !found || value != 1 {
		t.Fatal("rejected write changed existing value", value, found, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Put(canceled, "key", 2, cache.Forever()); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled put accepted", err)
	}
	if _, _, err := c.Get(nil, "key"); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted")
	}
	ctx, cancelDecode := context.WithCancel(t.Context())
	defer cancelDecode()
	d := cache.Define("cancel-decode", cache.StringKeys[string](), cache.NewCodec(func(v int) ([]byte, error) { return []byte("1"), nil }, func([]byte) (int, error) { cancelDecode(); return 9, nil }))
	bound, err := d.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Put(t.Context(), "key", 1, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	if value, found, err := bound.Get(ctx, "key"); !errors.Is(err, context.Canceled) || found || value != 0 {
		t.Fatal("canceled decoding published result", value, found, err)
	}
}
func TestReadBoundsAndTimeoutReachBackend(t *testing.T) {
	var decoded atomic.Int64
	codec := cache.NewCodec(func(int) ([]byte, error) { return []byte("0"), nil }, func([]byte) (int, error) { decoded.Add(1); return 1, nil })
	for _, mode := range []string{"oversized", "timeout", "missing"} {
		t.Run(mode, func(t *testing.T) {
			backend := readBackend{read: func(ctx context.Context, _ cache.EntryKey) ([]byte, bool, error) {
				switch mode {
				case "timeout":
					<-ctx.Done()
					return []byte("1"), true, ctx.Err()
				case "missing":
					return nil, false, nil
				}
				return []byte("oversized"), true, nil
			}}
			config := cache.DefaultConfig(namespace)
			config.MaxValueBytes = 3
			config.Timeout = 10 * time.Millisecond
			s, err := cache.NewStore(backend, config)
			if err != nil {
				t.Fatal(err)
			}
			c, err := cache.Define("read-limits", cache.StringKeys[string](), codec).Bind(s)
			if err != nil {
				t.Fatal(err)
			}
			value, found, err := c.Get(t.Context(), "key")
			if value != 0 || found {
				t.Fatal("invalid read published result")
			}
			// A value above the current decode bound is a miss that a write replaces.
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) || mode == "oversized" && err != nil || mode == "missing" && err != nil {
				t.Fatal("read failure classification", err)
			}
		})
	}
	if decoded.Load() != 0 {
		t.Fatal("invalid backend result reached decoder")
	}
}
func TestJSONCorruptionAndCustomBufferOwnership(t *testing.T) {
	for _, data := range []string{`{"Name":`, `{"Name":"ok"} {"Name":"extra"}`} {
		backend := readBackend{read: func(context.Context, cache.EntryKey) ([]byte, bool, error) { return []byte(data), true, nil }}
		s, _ := cache.NewStore(backend, cache.DefaultConfig(namespace))
		c, _ := profiles.Bind(s)
		if got, found, err := c.Get(t.Context(), "key"); err == nil || found || !reflect.DeepEqual(got, profile{}) {
			t.Fatal("corrupt cache data accepted")
		}
	}
	s, _, _ := store(t, nil)
	buffer := []byte("original")
	c, err := cache.Define("buffers", cache.StringKeys[string](), cache.NewCodec(func([]byte) ([]byte, error) { return buffer, nil }, func(data []byte) ([]byte, error) { return data, nil })).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "key", nil, cache.Forever()); err != nil {
		t.Fatal(err)
	}
	buffer[0] = 'X'
	got, _, err := c.Get(t.Context(), "key")
	if err != nil || string(got) != "original" {
		t.Fatal("encoder buffer retained")
	}
	got[0] = 'Y'
	again, _, err := c.Get(t.Context(), "key")
	if err != nil || string(again) != "original" {
		t.Fatal("decoder buffer aliases stored entry")
	}
}

func TestNaturalIntegerKeysAndPureConstruction(t *testing.T) {
	type SignedID int64
	type UnsignedID uint64
	s, _, _ := store(t, nil)
	signed, err := cache.Define("signed", cache.SignedKeys[SignedID](), cache.JSON[string]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := cache.Define("unsigned", cache.UnsignedKeys[UnsignedID](), cache.JSON[string]()).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []SignedID{-1 << 63, -1, 0, 1<<63 - 1} {
		if err := signed.Put(t.Context(), key, fmt.Sprint(key), cache.Forever()); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []SignedID{-1 << 63, -1, 0, 1<<63 - 1} {
		v, found, err := signed.Get(t.Context(), key)
		if err != nil || !found || v != fmt.Sprint(key) {
			t.Fatal("signed key lost precision", v, err)
		}
	}
	for _, key := range []UnsignedID{0, 1<<53 + 1, 1<<64 - 1} {
		if err := unsigned.Put(t.Context(), key, fmt.Sprint(key), cache.Forever()); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []UnsignedID{0, 1<<53 + 1, 1<<64 - 1} {
		v, found, err := unsigned.Get(t.Context(), key)
		if err != nil || !found || v != fmt.Sprint(key) {
			t.Fatal("unsigned key lost precision", v, err)
		}
	}
	calls := 0
	declaration := cache.Define("pure", cache.NewKeyCodec(func(k string) (string, error) { calls++; return k, nil }), cache.NewCodec(func(v int) ([]byte, error) { calls++; return nil, nil }, func([]byte) (int, error) { calls++; return 0, nil }))
	if _, err := declaration.Bind(s); err != nil || calls != 0 {
		t.Fatal("construction ran codecs", calls, err)
	}
}
func TestStoreConfigRejectsInvalidLimits(t *testing.T) {
	_, backend, _ := store(t, nil)
	for _, change := range []func(*cache.Config){func(c *cache.Config) { c.Namespace = cache.Namespace{} }, func(c *cache.Config) { c.MaxKeyBytes = 0 }, func(c *cache.Config) { c.MaxKeyBytes = cache.MaxKeyBytes + 1 }, func(c *cache.Config) { c.MaxValueBytes = 0 }, func(c *cache.Config) { c.MaxDeclarations = 0 }, func(c *cache.Config) { c.MaxBatchEntries = 0 }, func(c *cache.Config) { c.MaxBatchEntries = cache.MaxBatchEntries + 1 }, func(c *cache.Config) { c.MaxTags = 0 }, func(c *cache.Config) { c.MaxTags = cache.MaxTags + 1 }, func(c *cache.Config) { c.MaxFills = 0 }, func(c *cache.Config) { c.MaxFillWaiters = 0 }, func(c *cache.Config) { c.Timeout = 0 }} {
		config := cache.DefaultConfig(namespace)
		change(&config)
		if _, err := cache.NewStore(backend, config); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid store config accepted", err)
		}
	}
	if _, err := cache.NewStore(nil, cache.DefaultConfig(namespace)); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil backend accepted", err)
	}
}
