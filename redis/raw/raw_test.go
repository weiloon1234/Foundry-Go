package raw

import (
	"context"
	"errors"
	"math"
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
	calls    atomic.Int32
	request  Request
	requests []Request
	value    any
	values   []any
	err      error
}

func (b *fakeBackend) ExecuteRaw(ctx context.Context, r Request, l Limits) (Reply, error) {
	b.calls.Add(1)
	b.request = r
	if b.err != nil {
		return Reply{}, b.err
	}
	return CaptureReply(ctx, b.value, l.Reply)
}
func (b *fakeBackend) ExecuteRawBatch(ctx context.Context, r []Request, _ Mode, l Limits) ([]Reply, error) {
	b.calls.Add(1)
	b.requests = append([]Request(nil), r...)
	if b.err != nil {
		return nil, b.err
	}
	return CaptureReplies(ctx, b.values, l.Reply)
}
func rawTestStore(t *testing.T, b Backend, edit func(*Config)) (*Store, Key) {
	t.Helper()
	ns := keyspace.Namespace{Application: "raw-test", Environment: "test"}
	c := DefaultConfig(ns)
	if edit != nil {
		edit(&c)
	}
	s, err := NewStore(b, c)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewKey(ns, "values", 1, "a")
	if err != nil {
		t.Fatal(err)
	}
	return s, key
}
func TestImmutableCommandsPrefixKeysAndArguments(t *testing.T) {
	b := &fakeBackend{value: "OK"}
	store, key := rawTestStore(t, b, nil)
	input := []byte("value")
	original := NewCommand("xgroup", DecodeString()).Arg(Text("CREATE")).Key(key)
	command := original.Arg(Bytes(input))
	input[0] = 'X'
	if got, err := command.Run(t.Context(), store); err != nil || got != "OK" {
		t.Fatal(got, err)
	}
	args := b.request.Arguments()
	if strings.Join(args, "|") != "XGROUP|CREATE|"+key.String()+"|value" {
		t.Fatal(args)
	}
	args[1] = "changed"
	if b.request.Arguments()[1] != "CREATE" {
		t.Fatal("request arguments alias output")
	}
	if len(original.request.Arguments()) != 3 {
		t.Fatal("append mutated original")
	}
	script := NewScript("return ARGV[1]", key, DecodeString()).Arg(Text("hello")).Key(key)
	if got, err := script.Run(t.Context(), store); err != nil || got != "OK" || len(b.request.Keys()) != 2 || len(b.request.Arguments()) != 1 {
		t.Fatal(got, err, b.request.Keys())
	}
}
func TestRawValidationBeforeIO(t *testing.T) {
	b := &fakeBackend{value: int64(1)}
	store, key := rawTestStore(t, b, nil)
	other, _ := NewKey(keyspace.Namespace{Application: "foreign", Environment: "test"}, "values", 1, "a")
	for _, c := range []Command[int64]{
		{}, NewCommand("GET", DecodeInt64()).Key(Key{}), NewCommand("bad name", DecodeInt64()).Key(key),
		NewCommand(strings.Repeat("A", 65), DecodeInt64()).Key(key), NewCommand("GET", DecodeInt64()).Key(other),
		NewCommand("MGET", DecodeInt64()).Key(key).Key(other), NewCommand("SET", DecodeInt64()).Key(key).Arg(Argument{}),
		NewScript("", key, DecodeInt64()), NewScript(strings.Repeat("x", MaxScriptBytes+1), key, DecodeInt64()),
		NewCommand("SET", DecodeInt64()).Key(key).Arg(Text(strings.Repeat("x", MaxArgumentBytes+1))),
	} {
		if got, err := c.Run(t.Context(), store); err == nil || got != 0 {
			t.Fatal(got, err)
		}
	}
	for _, name := range []string{"SELECT", "CLIENT", "WATCH", "SUBSCRIBE", "MONITOR", "MULTI", "EVAL", "AUTH", "HELLO"} {
		if _, err := NewCommand(name, DecodeInt64()).Key(key).Run(t.Context(), store); !errors.Is(err, fault.Invalid) {
			t.Fatal(name, err)
		}
	}
	command := NewCommand("GET", DecodeInt64()).Key(key)
	for range MaxArguments {
		command = command.Arg(Text(""))
	}
	if _, err := command.Run(t.Context(), store); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewCommand("GET", DecodeInt64()).Key(key).Run(ctx, store); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid command reached backend")
	}
	p, _ := NewPipeline(Pipelined)
	first, err := Queue(p, NewCommand("INCR", DecodeInt64()).Key(key))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Queue(p, NewCommand("INCR", DecodeInt64()).Key(other)); err != nil {
		t.Fatal(err)
	}
	if err := p.Run(t.Context(), store); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if got, err := first.Value(); got != 0 || err == nil {
		t.Fatal(got, err)
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid later pipeline command allowed earlier I/O")
	}
}
func TestPipelineTypedResultsArePublishedTogether(t *testing.T) {
	b := &fakeBackend{values: []any{"OK", int64(3), []any{"owned"}}}
	store, key := rawTestStore(t, b, nil)
	p, _ := NewPipeline(Transaction)
	if err := Ignore(p, NewCommand("SET", DecodeString()).Key(key).Arg(Text("0"))); err != nil {
		t.Fatal(err)
	}
	count, err := Queue(p, NewCommand("INCRBY", DecodeInt64()).Key(key).Arg(Int64(3)))
	if err != nil {
		t.Fatal(err)
	}
	list, err := Queue(p, NewCommand("LRANGE", DecodeArray(DecodeString())).Key(key))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := count.Value(); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if err := p.Run(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if got, err := count.Value(); err != nil || got != 3 {
		t.Fatal(got, err)
	}
	if got, err := list.Value(); err != nil || len(got) != 1 || got[0] != "owned" {
		t.Fatal(got, err)
	}
	if err := p.Run(t.Context(), store); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if _, err := Queue(p, NewCommand("GET", DecodeString()).Key(key)); !errors.Is(err, fault.Conflict) {
		t.Fatal(err)
	}
	if b.calls.Load() != 1 {
		t.Fatal("pipeline repeated", b.calls.Load())
	}
	bad, _ := NewPipeline(Pipelined)
	a, _ := Queue(bad, NewCommand("GET", DecodeString()).Key(key))
	z, _ := Queue(bad, NewCommand("GET", DecodeString()).Key(key))
	b.values = []any{"ok", int64(2)}
	if err := bad.Run(t.Context(), store); err == nil {
		t.Fatal("bad later result accepted")
	}
	for _, r := range []Result[string]{a, z} {
		if value, err := r.Value(); value != "" || err == nil {
			t.Fatal(value, err)
		}
	}
}
func TestReplyOwnershipBoundsAndExactIntegers(t *testing.T) {
	input := []any{int64(math.MaxInt64), []any{"text", nil}}
	r, err := CaptureReply(t.Context(), input, DefaultReplyLimits())
	if err != nil {
		t.Fatal(err)
	}
	input[1].([]any)[0] = "changed"
	children, _ := r.Array()
	n, err := children[0].Integer()
	if err != nil || n != math.MaxInt64 {
		t.Fatal(n, err)
	}
	nested, _ := children[1].Array()
	text, err := nested[0].Text()
	if err != nil || text != "text" {
		t.Fatal(text, err)
	}
	children[0] = Reply{}
	again, _ := r.Array()
	if again[0].IsNull() {
		t.Fatal("child slices alias reply")
	}
	for _, input := range []any{true, float64(1), map[string]any{}, errors.New("secret server payload"), []any{errors.New("secret server payload")}} {
		if _, err := CaptureReply(t.Context(), input, DefaultReplyLimits()); err == nil || strings.Contains(err.Error(), "secret server payload") {
			t.Fatal(err)
		}
	}
	bounds := ReplyLimits{Bytes: 4, Nodes: 3, Depth: 1}
	for _, input := range []any{"12345", []any{int64(1), int64(2), int64(3)}, []any{[]any{int64(1)}}} {
		if _, err := CaptureReply(t.Context(), input, bounds); err == nil {
			t.Fatal("oversized reply", input)
		}
	}
	cycle := make([]any, 1)
	cycle[0] = cycle
	if _, err := CaptureReply(t.Context(), cycle, bounds); err == nil {
		t.Fatal("cycle accepted")
	}
	if _, err := CaptureReplies(t.Context(), []any{"123", "12"}, bounds); err == nil {
		t.Fatal("batch budget not shared")
	}
}
func TestRawCallbackFailureAndCancellationOwnership(t *testing.T) {
	for _, fail := range []func(){func() { panic("hidden codec value") }, runtime.Goexit} {
		b := &fakeBackend{value: int64(1)}
		store, key := rawTestStore(t, b, nil)
		decoder := DecodeWith(func(context.Context, Reply) (int, error) { fail(); return 0, nil })
		if got, err := NewCommand("GET", decoder).Key(key).Run(t.Context(), store); got != 0 || !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "hidden codec value") {
			t.Fatal(got, err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{value: int64(1)}
		store, key := rawTestStore(t, b, func(c *Config) { c.MaxConcurrent = 1; c.Timeout = time.Second })
		entered, release := make(chan struct{}), make(chan struct{})
		decoder := DecodeWith(func(context.Context, Reply) (int, error) { close(entered); <-release; return 1, nil })
		done := make(chan error, 1)
		go func() { _, err := NewCommand("GET", decoder).Key(key).Run(t.Context(), store); done <- err }()
		<-entered
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatal("decoder abandoned", err)
		default:
		}
		// A held slot makes the next operation queue briefly, then report overload.
		if _, err := NewCommand("GET", DecodeInt64()).Key(key).Run(t.Context(), store); !errors.Is(err, fault.Overloaded) {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}
func TestRawTypedKeysAndInvalidConfigurations(t *testing.T) {
	b := &fakeBackend{value: int64(1)}
	store, key := rawTestStore(t, b, func(c *Config) { c.MaxDeclarations = 1 })
	declaration := DefineKeys[string]("values", 1, keyspace.StringKeys[string]())
	keys, err := declaration.Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := declaration.Bind(store); err != nil {
		t.Fatal(err)
	}
	if _, err := DefineKeys[string]("values", 1, keyspace.StringKeys[string]()).Bind(store); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	if _, err := DefineKeys[string]("other", 1, keyspace.StringKeys[string]()).Bind(store); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if resolved, err := keys.For(t.Context(), "a"); err != nil || resolved != key {
		t.Fatal(resolved, err)
	}
	if yes, err := keys.Exists(t.Context(), "a"); err != nil || !yes {
		t.Fatal(yes, err)
	}
	if n, err := keys.DeleteMany(t.Context(), "a", "a"); err != nil || n != 1 || len(b.request.Keys()) != 1 {
		t.Fatal(n, err)
	}
	if n, err := keys.DeleteMany(t.Context()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := keys.Expire(t.Context(), "a", cache.TTL{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, edit := range []func(*Config){func(c *Config) { c.Namespace = keyspace.Namespace{} }, func(c *Config) { c.MaxConcurrent = 0 }, func(c *Config) { c.Timeout = 0 }, func(c *Config) { c.MaxDeclarations = 0 }, func(c *Config) { c.Limits.Arguments = MaxArguments + 1 }, func(c *Config) { c.Limits.Commands = MaxCommands + 1 }, func(c *Config) { c.Limits.Reply.Depth = MaxReplyDepth + 1 }} {
		c := DefaultConfig(key.Namespace())
		edit(&c)
		if _, err := NewStore(b, c); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestRawMaximumBoundsAndPipelineResultOwnership(t *testing.T) {
	b := &fakeBackend{}
	store, key := rawTestStore(t, b, func(c *Config) { c.Limits.Commands = MaxCommands })
	p, _ := NewPipeline(Transaction)
	b.values = make([]any, MaxCommands)
	var result Result[int64]
	for i := range MaxCommands {
		var err error
		result, err = Queue(p, NewCommand("INCR", DecodeInt64()).Key(key))
		if err != nil {
			t.Fatal(err)
		}
		b.values[i] = int64(i)
	}
	if p.Len() != MaxCommands || p.IsEmpty() || p.Mode() != Transaction {
		t.Fatal(p.Len(), p.Mode())
	}
	if _, err := Queue(p, NewCommand("INCR", DecodeInt64()).Key(key)); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := p.Run(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if v, err := result.Value(); err != nil || v != MaxCommands-1 || !p.IsEmpty() {
		t.Fatal(v, err)
	}
	for _, replyCount := range []int{0, 2} {
		one, _ := NewPipeline(Pipelined)
		r, _ := Queue(one, NewCommand("GET", DecodeString()).Key(key))
		b.values = make([]any, replyCount)
		if err := one.Run(t.Context(), store); !errors.Is(err, fault.Internal) {
			t.Fatal(err)
		}
		if v, err := r.Value(); v != "" || err == nil {
			t.Fatal(v, err)
		}
	}
	var tree any = "x"
	for range MaxReplyDepth {
		tree = []any{tree}
	}
	if _, err := CaptureReply(t.Context(), tree, ReplyLimits{Bytes: 1, Nodes: MaxReplyDepth + 1, Depth: MaxReplyDepth}); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureReply(t.Context(), []any{tree}, ReplyLimits{Bytes: 1, Nodes: MaxReplyDepth + 2, Depth: MaxReplyDepth}); err == nil {
		t.Fatal("maximum depth exceeded")
	}
	count := make([]any, MaxReplyNodes-1)
	for i := range count {
		count[i] = int64(i)
	}
	if _, err := CaptureReply(t.Context(), count, ReplyLimits{Bytes: 1, Nodes: MaxReplyNodes, Depth: 1}); err != nil {
		t.Fatal(err)
	}
}
func TestPipelineDecoderFailureRetainsOwnershipAndSuppressesAllResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &fakeBackend{values: []any{int64(1), int64(2)}}
		store, key := rawTestStore(t, b, func(c *Config) { c.MaxConcurrent = 1; c.Timeout = time.Second })
		entered, release := make(chan struct{}), make(chan struct{})
		p, _ := NewPipeline(Pipelined)
		first, _ := Queue(p, NewCommand("INCR", DecodeInt64()).Key(key))
		last, _ := Queue(p, NewCommand("INCR", DecodeWith(func(ctx context.Context, r Reply) (int64, error) { close(entered); <-release; return 2, nil })).Key(key))
		done := make(chan error, 1)
		go func() { done <- p.Run(t.Context(), store) }()
		<-entered
		time.Sleep(2 * time.Second)
		synctest.Wait()
		for _, r := range []Result[int64]{first, last} {
			if _, err := r.Value(); !errors.Is(err, fault.Conflict) {
				t.Fatal("incomplete pipeline exposed results", err)
			}
		}
		if err := p.Run(t.Context(), store); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		if _, err := Queue(p, NewCommand("GET", DecodeString()).Key(key)); !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		for _, r := range []Result[int64]{first, last} {
			if v, err := r.Value(); v != 0 || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(v, err)
			}
		}
	})
}
