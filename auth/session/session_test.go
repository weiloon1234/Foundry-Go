package session

import (
	"context"
	"encoding/base64"
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
	return model.NewReference[member]("members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }
func provider() auth.Provider[member, int64] {
	return auth.DefineProvider("members", (member{}).reference(), func(_ context.Context, id int64) (value.Optional[member], error) {
		return value.Set(member{ID: id}), nil
	}, func(context.Context, member) (bool, error) { return true, nil })
}
func proof(t *testing.T) auth.Proof[member, int64] {
	t.Helper()
	p, err := auth.NewProof(member{7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func settings() Config {
	return DefaultConfig(keyspace.Namespace{Application: "session-tests", Environment: "test"})
}

var instant = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

type fakeBackend struct {
	Backend
	create func(context.Context, Address, Creation) (Record, error)
	lookup func(context.Context, Address, Digest, bool) (value.Optional[Record], error)
	list   func(context.Context, Address, model.Identity, int) ([]Record, error)
}

func (b *fakeBackend) Create(c context.Context, a Address, r Creation) (Record, error) {
	return b.create(c, a, r)
}
func (b *fakeBackend) Lookup(c context.Context, a Address, h Digest, touch bool) (value.Optional[Record], error) {
	return b.lookup(c, a, h, touch)
}
func (b *fakeBackend) List(c context.Context, a Address, i model.Identity, n int) ([]Record, error) {
	return b.list(c, a, i, n)
}
func binding(t *testing.T, backend Backend, config Config) *Sessions[member, int64] {
	t.Helper()
	store, err := NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store, "web", provider(), "session")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func creation(t *testing.T) (Address, Creation) {
	t.Helper()
	p := proof(t)
	id, err := model.NewID[Record]()
	if err != nil {
		t.Fatal(err)
	}
	_, hash, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	a := Address{Namespace: settings().Namespace, Guard: "web", Provider: "members", Model: "members"}
	return a, Creation{ID: id, Subject: p.Identity(), Hash: hash, Assurance: auth.Authenticated, Lifetime: Lifetime{Idle: 10 * time.Minute, Absolute: 20 * time.Minute, Sliding: true}, Maximum: 2}
}

func TestCanonicalSecretAndSafeFormatting(t *testing.T) {
	token, hash, err := newSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(token.Reveal()) != EncodedSecretBytes {
		t.Fatal("incorrect entropy encoding")
	}
	got, err := HashSecret(token)
	if err != nil || !got.Equal(hash) {
		t.Fatal("digest roundtrip", err)
	}
	parsed, err := ParseDigest(hash.Hex())
	if err != nil || !parsed.Equal(hash) {
		t.Fatal("stored digest roundtrip", err)
	}
	bad := []string{"", token.Reveal() + "=", token.Reveal() + "\n", strings.Repeat("!", EncodedSecretBytes), strings.Repeat("A", 42) + "B"}
	for _, input := range bad {
		if got, err := HashSecret(secret.New(input)); !errors.Is(err, auth.Unauthenticated) || !got.IsZero() {
			t.Fatal("accepted malformed secret")
		}
	}
	for _, input := range []string{"", strings.ToUpper(hash.Hex()), strings.Repeat("0", 64), hash.Hex() + "0"} {
		if got, err := ParseDigest(input); err == nil || !got.IsZero() {
			t.Fatal("accepted malformed digest")
		}
	}
	issued := Issued[member, int64]{secret: token}
	for _, v := range []any{issued, hash, Record{Hash: hash}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			out := fmt.Sprintf(format, v)
			if strings.Contains(out, token.Reveal()) || strings.Contains(out, hash.Hex()) {
				t.Fatal("secret formatting leak")
			}
		}
	}
	data, err := json.Marshal(issued)
	if err != nil || string(data) != "{}" {
		t.Fatal("issued JSON exposed credentials", err)
	}
}
func FuzzHashSecret(f *testing.F) {
	f.Add("")
	f.Add(base64.RawURLEncoding.EncodeToString(make([]byte, SecretBytes)))
	f.Add(strings.Repeat("A", 42) + "B")
	f.Fuzz(func(t *testing.T, input string) {
		got, err := HashSecret(secret.New(input))
		if err != nil {
			if !got.IsZero() {
				t.Fatal("partial digest")
			}
			return
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(input)
		if err != nil || len(raw) != SecretBytes || base64.RawURLEncoding.EncodeToString(raw) != input {
			t.Fatal("accepted noncanonical secret")
		}
	})
}
func TestLifetimeTouchAndAddressIsolation(t *testing.T) {
	a, c := creation(t)
	r, err := c.At(a, instant)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{9 * time.Minute, 18 * time.Minute} {
		next, live, err := r.Touch(instant.Add(step))
		if err != nil || !live || next.ExpiresAt != r.ExpiresAt {
			t.Fatal("touch extended absolute expiry", err)
		}
		r = next
	}
	if r.IdleExpiresAt != r.ExpiresAt {
		t.Fatal("idle deadline exceeded absolute")
	}
	if back, live, err := r.Touch(instant); err != nil || !live || back != r {
		t.Fatal("backward clock changed activity", err)
	}
	if expired, live, err := r.Touch(instant.Add(20 * time.Minute)); err != nil || live || expired != (Record{}) {
		t.Fatal("expired session revived", err)
	}
	c.Lifetime.Sliding = false
	r, err = c.At(a, instant)
	if err != nil {
		t.Fatal(err)
	}
	if got, live, err := r.Touch(instant.Add(time.Minute)); err != nil || !live || got != r {
		t.Fatal("fixed deadline slid", err)
	}
	scope, _ := a.Key()
	for _, change := range []func(*Address){func(a *Address) { a.Guard = "api" }, func(a *Address) { a.Provider = "admins" }, func(a *Address) { a.Model = "admins" }, func(a *Address) { a.Namespace.Environment = "production" }, func(a *Address) { a.Namespace.Application = "other" }} {
		other := a
		change(&other)
		key, err := other.Key()
		if err != nil || key == scope {
			t.Fatal("address collision", err)
		}
		if r.Validate(other) == nil {
			t.Fatal("foreign record accepted")
		}
	}
	c.Assurance = auth.PendingMFA
	c.Remember = true
	if c.Validate(a) == nil {
		t.Fatal("remembered pending MFA accepted")
	}
}
func TestInvalidConfigAndTypedNilBackend(t *testing.T) {
	if _, err := NewStore((*fakeBackend)(nil), settings()); err == nil {
		t.Fatal("typed nil backend")
	}
	for _, change := range []func(*Config){func(c *Config) { c.Regular.Idle = 0 }, func(c *Config) { c.Regular.Idle = time.Millisecond + 1 }, func(c *Config) { c.Regular.Absolute = MaxLifetime + time.Second }, func(c *Config) { c.Pending.Sliding = true }, func(c *Config) { c.MaxPerSubject = MaxSessions + 1 }, func(c *Config) { c.MaxConcurrent = 0 }, func(c *Config) { c.Timeout = 0 }} {
		c := settings()
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
func TestIssueOwnsCanceledCallbackAndDiscardsLateCredential(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int32
	b := &fakeBackend{create: func(_ context.Context, a Address, c Creation) (Record, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return c.At(a, instant)
	}}
	config := settings()
	config.MaxConcurrent = 1
	s := binding(t, b, config)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	p := proof(t)
	go func() {
		got, err := s.Issue(ctx, p, IssueOptions{})
		if !got.Secret().IsZero() {
			t.Error("canceled operation published secret")
		}
		done <- err
	}()
	<-entered
	cancel()
	if got, err := s.Issue(t.Context(), p, IssueOptions{}); !errors.Is(err, fault.Conflict) || !got.Secret().IsZero() {
		t.Fatal("released owned callback slot", err)
	}
	select {
	case <-done:
		t.Fatal("callback abandoned")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Issue(t.Context(), p, IssueOptions{}); err != nil {
		t.Fatal("slot not released on actual exit", err)
	}
}
func TestAbnormalCallbackAndCorruptResultFailWithoutCredential(t *testing.T) {
	sentinel := errors.New("private backend detail")
	for name, fn := range map[string]func(context.Context, Address, Creation) (Record, error){
		"panic":  func(context.Context, Address, Creation) (Record, error) { panic("secret panic") },
		"goexit": func(context.Context, Address, Creation) (Record, error) { runtime.Goexit(); return Record{}, nil },
		"error": func(_ context.Context, a Address, c Creation) (Record, error) {
			r, _ := c.At(a, instant)
			return r, sentinel
		},
		"wrong-subject": func(_ context.Context, a Address, c Creation) (Record, error) {
			r, _ := c.At(a, instant)
			r.Subject, _ = member{8}.FoundryIdentity()
			return r, nil
		},
		"wrong-hash": func(_ context.Context, a Address, c Creation) (Record, error) {
			r, _ := c.At(a, instant)
			_, r.Hash, _ = newSecret()
			return r, nil
		},
		"wrong-assurance": func(_ context.Context, a Address, c Creation) (Record, error) {
			r, _ := c.At(a, instant)
			r.Assurance = auth.PendingMFA
			return r, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := &fakeBackend{create: fn}
			s := binding(t, b, settings())
			p := proof(t)
			got, err := s.Issue(t.Context(), p, IssueOptions{})
			if err == nil || !got.Secret().IsZero() || !got.Info().ID().IsZero() {
				t.Fatal("partial credential on failure")
			}
			if strings.Contains(err.Error(), "secret panic") || strings.Contains(err.Error(), sentinel.Error()) {
				t.Fatal("backend error leaked")
			}
			b.create = func(_ context.Context, a Address, c Creation) (Record, error) { return c.At(a, instant) }
			if _, err := s.Issue(t.Context(), p, IssueOptions{}); err != nil {
				t.Fatal("callback slot leaked", err)
			}
		})
	}
}
func TestMalformedSecretNeverCallsBackendAndInvalidListIsDiscarded(t *testing.T) {
	var calls atomic.Int32
	b := &fakeBackend{lookup: func(context.Context, Address, Digest, bool) (value.Optional[Record], error) {
		calls.Add(1)
		return value.Optional[Record]{}, nil
	}}
	s := binding(t, b, settings())
	if _, err := s.verify(t.Context(), secret.New("malformed")); !errors.Is(err, auth.Unauthenticated) || calls.Load() != 0 {
		t.Fatal("malformed input reached store")
	}
	a, c := creation(t)
	r, err := c.At(a, instant)
	if err != nil {
		t.Fatal(err)
	}
	b.list = func(context.Context, Address, model.Identity, int) ([]Record, error) { return []Record{r, r}, nil }
	if rows, err := s.List(t.Context(), member{7}.reference()); err == nil || rows != nil {
		t.Fatal("duplicate metadata exposed")
	}
	r.Subject, _ = member{8}.FoundryIdentity()
	b.list = func(context.Context, Address, model.Identity, int) ([]Record, error) { return []Record{r}, nil }
	if rows, err := s.List(t.Context(), member{7}.reference()); err == nil || rows != nil {
		t.Fatal("foreign metadata exposed")
	}
}

func TestScopedProofCannotBecomeAnUnscopedSession(t *testing.T) {
	var calls atomic.Int32
	backend := &fakeBackend{create: func(context.Context, Address, Creation) (Record, error) { calls.Add(1); return Record{}, nil }}
	sessions := binding(t, backend, settings())
	grant, err := auth.NewAccessScopes(auth.DefineAccessScope[member]("orders.read"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scopes := range []auth.AccessScopes[member]{grant, {}} {
		proof, err := auth.NewScopedProof(member{7}.reference(), auth.Authenticated, scopes)
		if err != nil {
			t.Fatal(err)
		}
		issued, err := sessions.Issue(t.Context(), proof, IssueOptions{})
		if !errors.Is(err, fault.Invalid) || !issued.Secret().IsZero() {
			t.Fatal("scoped proof upgraded to session", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("scoped proof reached session storage")
	}
}
