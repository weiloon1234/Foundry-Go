package token

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type member struct{ ID int64 }

func (m member) reference() model.Reference[member, int64] {
	return model.NewReference[member]("token_members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

var instant = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func settings() Config {
	return DefaultConfig(keyspace.Namespace{Application: "tokens", Environment: "test"})
}
func scopes(t *testing.T, names ...auth.AccessScopeName) auth.AccessScopes[member] {
	t.Helper()
	items := make([]auth.AccessScope[member], len(names))
	for i, name := range names {
		items[i] = auth.DefineAccessScope[member](name)
	}
	result, err := auth.NewAccessScopes(items...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func proof(t *testing.T, state auth.Assurance) auth.Proof[member, int64] {
	t.Helper()
	p, err := auth.NewProof(member{7}.reference(), state)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type fakeBackend struct {
	Backend
	create  func(context.Context, Address, Creation) (Record, error)
	lookup  func(context.Context, Address, Digest, bool) (value.Optional[Record], error)
	refresh func(context.Context, Address, Digest, Digest, Digest) (value.Optional[Record], error)
}

func (b *fakeBackend) Create(ctx context.Context, a Address, c Creation) (Record, error) {
	return b.create(ctx, a, c)
}
func (b *fakeBackend) Lookup(ctx context.Context, a Address, h Digest, touch bool) (value.Optional[Record], error) {
	return b.lookup(ctx, a, h, touch)
}
func (b *fakeBackend) Refresh(ctx context.Context, a Address, old, access, refresh Digest) (value.Optional[Record], error) {
	return b.refresh(ctx, a, old, access, refresh)
}
func binding(t *testing.T, backend Backend, config Config) *Tokens[member, int64] {
	t.Helper()
	provider := auth.DefineProvider("members", member{}.reference(), func(_ context.Context, id int64) (value.Optional[member], error) { return value.Set(member{id}), nil }, func(context.Context, member) (bool, error) { return true, nil })
	store, err := NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := New(store, "api", provider, "bearer", scopes(t, "orders.read", "orders.write"))
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestIssuedTokenIsHashedScopedAndRedacted(t *testing.T) {
	var saved Record
	var calls atomic.Int32
	backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
		var err error
		saved, err = c.At(a, instant)
		return saved, err
	}, lookup: func(_ context.Context, _ Address, h Digest, touch bool) (value.Optional[Record], error) {
		calls.Add(1)
		if touch {
			t.Error("guard lookup touched activity")
		}
		if !saved.AccessHash.Equal(h) {
			return value.Optional[Record]{}, nil
		}
		return value.Set(saved), nil
	}}
	tokens := binding(t, backend, settings())
	issued, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Name: "Phone", Scopes: scopes(t, "orders.read"), Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	refresh, present := issued.RefreshSecret().Get()
	if !present || refresh.IsZero() || issued.AccessSecret().IsZero() || refresh.Reveal() == issued.AccessSecret().Reveal() {
		t.Fatal("invalid token pair")
	}
	for _, v := range []any{issued, issued.Info(), saved, saved.AccessHash} {
		encoded, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprintf("%#v %s", v, encoded), issued.AccessSecret().Reveal()) || strings.Contains(fmt.Sprint(v), refresh.Reveal()) {
			t.Fatal("credential disclosed")
		}
	}
	registry, err := auth.NewRegistry(auth.DefaultConfig(), tokens.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "bearer", Secret: issued.AccessSecret()})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	for range 3 {
		a, err := tokens.Guard().RequireScopes(scope.Context(), scopes(t, "orders.read"))
		if err != nil || a.ID != 7 {
			t.Fatal(a, err)
		}
	}
	if _, err := tokens.Guard().RequireScopes(scope.Context(), scopes(t, "orders.write")); !errors.Is(err, auth.Forbidden) {
		t.Fatal("excess scope", err)
	}
	if calls.Load() != 1 {
		t.Fatal("repeated token verification")
	}
}

func TestScopedIssuanceCannotExpandAndMFAIsRestricted(t *testing.T) {
	var calls atomic.Int32
	backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) { calls.Add(1); return c.At(a, instant) }}
	tokens := binding(t, backend, settings())
	restricted, err := auth.NewScopedProof(member{7}.reference(), auth.Authenticated, scopes(t, "orders.read"))
	if err != nil {
		t.Fatal(err)
	}
	if issued, err := tokens.Issue(t.Context(), restricted, IssueOptions[member]{Scopes: scopes(t, "orders.write")}); !errors.Is(err, auth.Forbidden) || !issued.AccessSecret().IsZero() {
		t.Fatal("expanded grant", err)
	}
	if _, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Scopes: scopes(t, "undeclared")}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("undeclared grant", err)
	}
	for _, option := range []IssueOptions[member]{{Refresh: true}, {Scopes: scopes(t, "orders.read")}} {
		if _, err := tokens.Issue(t.Context(), proof(t, auth.PendingMFA), option); !errors.Is(err, fault.Invalid) {
			t.Fatal("pending escalated", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid issuance reached persistence")
	}
	issued, err := tokens.Issue(t.Context(), proof(t, auth.PendingMFA), IssueOptions[member]{})
	if err != nil {
		t.Fatal(err)
	}
	if issued.RefreshSecret().IsSet() || issued.Info().Assurance() != auth.PendingMFA || issued.Info().Mode() != Challenge {
		t.Fatal("invalid challenge metadata")
	}
}

func TestBackendCannotMutateTheRequestedScopeSnapshot(t *testing.T) {
	backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
		c.Scopes[0] = "orders.write"
		return c.At(a, instant)
	}}
	tokens := binding(t, backend, settings())
	issued, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Scopes: scopes(t, "orders.read")})
	if !errors.Is(err, fault.Invalid) || !issued.AccessSecret().IsZero() {
		t.Fatal("backend changed immutable issuance grant", err)
	}
}

func TestCanceledIssuanceOwnsCapacityAndDiscardsLateSecret(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
		close(started)
		<-release
		return c.At(a, instant)
	}}
	config := settings()
	config.MaxConcurrent = 1
	tokens := binding(t, backend, config)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		issued, err := tokens.Issue(ctx, proof(t, auth.Authenticated), IssueOptions[member]{})
		if !issued.AccessSecret().IsZero() {
			t.Error("late secret escaped")
		}
		result <- err
	}()
	<-started
	cancel()
	if _, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{}); !errors.Is(err, fault.Conflict) {
		t.Error("canceled callback released capacity early", err)
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRefreshPreservesGrantAbsoluteExpiryAndBounds(t *testing.T) {
	backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) { return c.At(a, instant) }}
	tokens := binding(t, backend, settings())
	identity, err := member{7}.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[Record]()
	if err != nil {
		t.Fatal(err)
	}
	_, access, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	_, refresh, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	creation := Creation{ID: id, Subject: identity, Scopes: []auth.AccessScopeName{"orders.read"}, Mode: Renewable, Assurance: auth.Authenticated, AccessHash: access, RefreshHash: value.Set(refresh), Lifetime: Lifetime{Access: 10 * time.Minute, RefreshIdle: 30 * time.Minute, Absolute: time.Hour}, RotationLimit: 2, Maximum: 2}
	current, err := creation.At(tokens.address, instant)
	if err != nil {
		t.Fatal(err)
	}
	for _, advance := range []time.Duration{20 * time.Minute, 45 * time.Minute} {
		_, a, _ := newSecret()
		_, r, _ := newSecret()
		next, live, err := current.Refreshed(instant.Add(advance), a, r)
		if err != nil || !live {
			t.Fatal(live, err)
		}
		if next.ExpiresAt != current.ExpiresAt || next.ID != current.ID || next.Generation != current.Generation+1 {
			t.Fatal("refresh reset identity/lifetime")
		}
		next.Scopes[0] = "changed"
		if current.Scopes[0] != "orders.read" {
			t.Fatal("refresh aliased grants")
		}
		next.Scopes[0] = "orders.read"
		current = next
	}
	_, a, _ := newSecret()
	_, r, _ := newSecret()
	if _, live, err := current.Refreshed(instant.Add(46*time.Minute), a, r); err != nil || live {
		t.Fatal("rotation history was unbounded", err)
	}
	if _, live, err := current.Refreshed(instant.Add(time.Hour), a, r); err != nil || live {
		t.Fatal("absolute deadline revived", err)
	}
	if _, _, err := current.Refreshed(instant.Add(46*time.Minute), a, a); !errors.Is(err, fault.Invalid) {
		t.Fatal("shared access/refresh hash", err)
	}
}

func TestMalformedTokenDoesNotReachBackend(t *testing.T) {
	var calls atomic.Int32
	backend := &fakeBackend{lookup: func(context.Context, Address, Digest, bool) (value.Optional[Record], error) {
		calls.Add(1)
		return value.Optional[Record]{}, nil
	}}
	tokens := binding(t, backend, settings())
	if _, err := tokens.Refresh(t.Context(), secret.New("bad")); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if _, err := tokens.Touch(t.Context(), secret.New("bad")); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("malformed input reached backend")
	}
}

func TestUncertainTokenWritesDiscardSecretsWithoutRetry(t *testing.T) {
	uncertain := errors.New("commit acknowledgement lost")
	for _, operation := range []string{"issue", "refresh"} {
		t.Run(operation, func(t *testing.T) {
			var current Record
			var writes int
			backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
				writes++
				var err error
				current, err = c.At(a, instant)
				if err != nil {
					return Record{}, err
				}
				if operation == "issue" {
					return current, uncertain
				}
				return current, nil
			}, refresh: func(_ context.Context, _ Address, old, access, refresh Digest) (value.Optional[Record], error) {
				writes++
				next, live, err := current.Refreshed(instant.Add(time.Minute), access, refresh)
				if err != nil || !live {
					return value.Optional[Record]{}, err
				}
				return value.Set(next), uncertain
			}}
			tokens := binding(t, backend, settings())
			issued, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Refresh: true})
			expected := 1
			if operation == "refresh" {
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := issued.RefreshSecret().Get()
				issued, err = tokens.Refresh(t.Context(), raw)
				expected = 2
			}
			if !errors.Is(err, uncertain) || writes != expected || !issued.AccessSecret().IsZero() || issued.RefreshSecret().IsSet() || !issued.Info().ID().IsZero() {
				t.Fatal("uncertain write returned a credential or retried", err, writes)
			}
		})
	}
}
func TestTokenCallbacksReleaseCapacityAfterPanicAndGoexit(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
				calls++
				if calls == 1 {
					if mode == "panic" {
						panic("private backend detail")
					}
					runtime.Goexit()
				}
				return c.At(a, instant)
			}}
			config := settings()
			config.MaxConcurrent = 1
			tokens := binding(t, backend, config)
			issued, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{})
			if err == nil || !issued.AccessSecret().IsZero() {
				t.Fatal("callback failure returned credential")
			}
			next, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{})
			if err != nil || next.AccessSecret().IsZero() || calls != 2 {
				t.Fatal("failed callback retained capacity", err)
			}
		})
	}
}
func TestTokenIssuanceRejectsCorruptBackendMetadata(t *testing.T) {
	for _, corrupt := range []struct {
		name  string
		apply func(*Record)
	}{
		{"subject", func(r *Record) { r.Subject, _ = member{8}.FoundryIdentity() }},
		{"identifier", func(r *Record) { r.ID = model.ID[Record]{} }},
		{"access", func(r *Record) { _, r.AccessHash, _ = newSecret() }},
		{"refresh", func(r *Record) { r.RefreshHash = value.Optional[Digest]{} }},
		{"generation", func(r *Record) { r.Generation = 1 }},
		{"assurance", func(r *Record) { r.Assurance = auth.PendingMFA }},
	} {
		t.Run(corrupt.name, func(t *testing.T) {
			backend := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
				r, err := c.At(a, instant)
				if err != nil {
					return Record{}, err
				}
				corrupt.apply(&r)
				return r, nil
			}}
			tokens := binding(t, backend, settings())
			got, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Refresh: true})
			if !errors.Is(err, fault.Invalid) || !got.AccessSecret().IsZero() || got.RefreshSecret().IsSet() {
				t.Fatal("corrupt issuance published", err)
			}
		})
	}
}
