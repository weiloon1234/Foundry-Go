package token

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// memoryBackend is a single-family backend for runtime tests of the binding.
type memoryBackend struct {
	fakeBackend
	saved   Record
	removed []model.ID[Record]
	others  func(model.ID[Record]) uint64
}

func (b *memoryBackend) List(context.Context, Address, model.Identity, int) ([]Record, error) {
	return []Record{b.saved}, nil
}
func (b *memoryBackend) RevokeID(_ context.Context, _ Address, _ model.Identity, id model.ID[Record]) (bool, error) {
	b.removed = append(b.removed, id)
	return true, nil
}
func (b *memoryBackend) RevokeOthers(_ context.Context, _ Address, _ model.Identity, keep model.ID[Record]) (uint64, error) {
	return b.others(keep), nil
}

func newMemoryBackend() *memoryBackend {
	b := &memoryBackend{}
	b.create = func(_ context.Context, a Address, c Creation) (Record, error) {
		var err error
		b.saved, err = c.At(a, time.Now().UTC().Truncate(time.Microsecond))
		return b.saved, err
	}
	b.lookup = func(_ context.Context, _ Address, h Digest, _ bool) (value.Optional[Record], error) {
		if !b.saved.AccessHash.Equal(h) {
			return value.Optional[Record]{}, nil
		}
		return value.Set(b.saved), nil
	}
	b.refresh = func(_ context.Context, _ Address, old, access, refresh Digest) (value.Optional[Record], error) {
		current, present := b.saved.RefreshHash.Get()
		if !present || !current.Equal(old) {
			return value.Optional[Record]{}, nil
		}
		next, live, err := b.saved.Refreshed(time.Now().UTC().Truncate(time.Microsecond), access, refresh)
		if err != nil || !live {
			return value.Optional[Record]{}, err
		}
		b.saved = next
		return value.Set(next), nil
	}
	return b
}

func bindingWithin(t *testing.T, backend Backend, config Config, ceiling auth.AccessScopes[member]) *Tokens[member, int64] {
	t.Helper()
	provider := auth.DefineProvider("members", member{}.reference(), func(_ context.Context, id int64) (value.Optional[member], error) { return value.Set(member{id}), nil }, func(context.Context, member) (bool, error) { return true, nil })
	store, err := NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := New(store, "api", provider, "bearer", ceiling)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func authenticated(t *testing.T, tokens *Tokens[member, int64], access secret.String) context.Context {
	t.Helper()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), tokens.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "bearer", Secret: access})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Close() })
	return scope.Context()
}

func mustInstant(t *testing.T, at time.Time) temporal.DateTime {
	t.Helper()
	instant, err := temporal.NewDateTime(at)
	if err != nil {
		t.Fatal(err)
	}
	return instant
}

// Removing a scope from the binding's ceiling withdraws it from stored tokens
// without making them unusable, unlistable or unrefreshable. Before, List
// failed and Refresh committed the rotation and then returned Unauthenticated.
func TestNarrowedCeilingIntersectsStoredScopes(t *testing.T) {
	backend := newMemoryBackend()
	wide := bindingWithin(t, backend, settings(), scopes(t, "orders.read", "orders.write"))
	issued, err := wide.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Scopes: scopes(t, "orders.read", "orders.write"), Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	narrow := bindingWithin(t, backend, settings(), scopes(t, "orders.read"))
	ctx := authenticated(t, narrow, issued.AccessSecret())
	if _, err := narrow.Guard().RequireScopes(ctx, scopes(t, "orders.read")); err != nil {
		t.Fatal("narrowed token stopped authenticating", err)
	}
	if _, err := narrow.Guard().RequireScopes(ctx, scopes(t, "orders.write")); !errors.Is(err, auth.Forbidden) {
		t.Fatal("withdrawn scope still granted", err)
	}
	listed, err := narrow.List(t.Context(), member{7}.reference())
	if err != nil || len(listed) != 1 || !listed[0].Scopes().ContainsAll(scopes(t, "orders.read")) || listed[0].Scopes().ContainsAll(scopes(t, "orders.write")) {
		t.Fatal("listing did not apply the current ceiling", err)
	}
	raw, _ := issued.RefreshSecret().Get()
	refreshed, err := narrow.Refresh(t.Context(), raw)
	if err != nil || refreshed.AccessSecret().IsZero() || refreshed.Info().Scopes().ContainsAll(scopes(t, "orders.write")) {
		t.Fatal("refresh with a narrowed ceiling failed after committing", err)
	}
	// The stored grant is never widened or rewritten by a narrower binding.
	if len(backend.saved.Scopes) != 2 {
		t.Fatal("stored grant was rewritten")
	}
}

// A committed refresh must reach the caller even when the operation deadline
// passes after the backend returned: the old refresh token is consumed.
func TestCommittedRefreshSurvivesLateDeadline(t *testing.T) {
	backend := newMemoryBackend()
	config := settings()
	config.Timeout = 50 * time.Millisecond
	tokens := bindingWithin(t, backend, config, scopes(t, "orders.read"))
	issued, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	commit := backend.refresh
	backend.refresh = func(ctx context.Context, a Address, old, access, refresh Digest) (value.Optional[Record], error) {
		result, err := commit(ctx, a, old, access, refresh)
		<-ctx.Done() // The deadline passes after the backend committed.
		return result, err
	}
	raw, _ := issued.RefreshSecret().Get()
	refreshed, err := tokens.Refresh(t.Context(), raw)
	if err != nil || refreshed.AccessSecret().IsZero() || refreshed.Info().Generation() != 1 {
		t.Fatal("committed refresh result was lost", err)
	}
}

// Handlers read the verified token without a second lookup, revoke it or the
// subject's other tokens, and mint tokens that inherit only its grants.
func TestCurrentTokenOperationsAndEvents(t *testing.T) {
	backend := newMemoryBackend()
	base := bindingWithin(t, backend, settings(), scopes(t, "orders.read", "orders.write"))
	var events []auth.Event
	tokens, err := base.WithObserver(func(_ context.Context, event auth.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.WithObserver(func(context.Context, auth.Event) {}); !errors.Is(err, fault.Duplicate) {
		t.Fatal("second observer accepted", err)
	}
	request := attribution.Request{IP: netip.MustParseAddr("192.0.2.9"), UserAgent: "Phone/1 " + strings.Repeat("é", auth.MaxDeviceUserAgentBytes)}
	origin, err := attribution.Origin{}.WithRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	issuing, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := tokens.Issue(issuing, proof(t, auth.Authenticated), IssueOptions[member]{Scopes: scopes(t, "orders.read")})
	if err != nil {
		t.Fatal(err)
	}
	device := issued.Info().Device()
	if device.ClientIP != request.IP || len(device.UserAgent) > auth.MaxDeviceUserAgentBytes || !strings.HasPrefix(device.UserAgent, "Phone/1 ") || strings.ContainsRune(device.UserAgent, '�') {
		t.Fatal("device metadata was not captured within bounds", device)
	}
	if len(events) != 1 || events[0].Kind != auth.EventLogin || events[0].Request.IP != request.IP {
		t.Fatal("login was not observed", events)
	}
	ctx := authenticated(t, tokens, issued.AccessSecret())
	current, err := tokens.Current(ctx)
	if err != nil || current.ID() != issued.Info().ID() {
		t.Fatal("current token metadata", err)
	}
	inherited, err := tokens.CurrentProof(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.Issue(t.Context(), inherited, IssueOptions[member]{Scopes: scopes(t, "orders.write")}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("current proof widened the request's grants", err)
	}
	backend.others = func(keep model.ID[Record]) uint64 {
		if keep != current.ID().value {
			t.Error("revoked the current token")
		}
		return 3
	}
	if count, err := tokens.RevokeOthers(ctx); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if removed, err := tokens.RevokeCurrent(ctx); err != nil || !removed || len(backend.removed) != 1 || backend.removed[0] != current.ID().value {
		t.Fatal("current token was not revoked", err)
	}
	if len(events) != 3 || events[1].Kind != auth.EventOtherDevicesLoggedOut || events[1].Count != 3 || events[2].Kind != auth.EventLogout {
		t.Fatal("revocations were not observed", events)
	}
	if _, err := tokens.Current(t.Context()); err == nil {
		t.Fatal("current token outside an auth scope")
	}
}

// Prefixed secrets support secret scanning; only the random part is hashed, so
// adding or changing the prefix never invalidates existing tokens.
func TestTokenPrefixIsValidatedAndOptionalOnInput(t *testing.T) {
	for _, prefix := range []string{"Upper_", "has-dash", "sixteen_bytes_ok_", "é"} {
		config := settings()
		config.Prefix = prefix
		if config.Validate() == nil {
			t.Fatal("invalid prefix accepted", prefix)
		}
	}
	config := settings()
	config.Prefix = "acme_pat_"
	backend := newMemoryBackend()
	tokens := bindingWithin(t, backend, config, scopes(t, "orders.read"))
	issued, err := tokens.Issue(t.Context(), proof(t, auth.Authenticated), IssueOptions[member]{Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	access := issued.AccessSecret().Reveal()
	refresh, _ := issued.RefreshSecret().Get()
	if !strings.HasPrefix(access, "acme_pat_") || !strings.HasPrefix(refresh.Reveal(), "acme_pat_") {
		t.Fatal("issued secrets are not prefixed")
	}
	prefixed, err := HashSecret(issued.AccessSecret())
	if err != nil {
		t.Fatal(err)
	}
	bare, err := HashSecret(secret.New(strings.TrimPrefix(access, "acme_pat_")))
	if err != nil || !bare.Equal(prefixed) {
		t.Fatal("prefix changed the stored digest", err)
	}
	for _, invalid := range []string{"ACME_" + strings.TrimPrefix(access, "acme_pat_"), "acme_pat_" + access} {
		if _, err := HashSecret(secret.New(invalid)); !errors.Is(err, auth.Unauthenticated) {
			t.Fatal("malformed prefixed secret accepted", err)
		}
	}
	for _, grace := range []time.Duration{-time.Second, settings().Renewable.Access + time.Second} {
		config := settings()
		config.AccessGrace = grace
		if config.Validate() == nil {
			t.Fatal("invalid access grace accepted", grace)
		}
	}
}

// A superseded generation authenticates only within the configured grace.
func TestSupersededGenerationHonorsGrace(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := Record{AccessExpiresAt: mustInstant(t, now.Add(10*time.Minute)), ExpiresAt: mustInstant(t, now.Add(time.Hour)), SupersededAt: value.Set(mustInstant(t, now))}
	if !record.LiveAccessWithin(now.Add(29*time.Second), 30*time.Second) || record.LiveAccessWithin(now.Add(30*time.Second), 30*time.Second) {
		t.Fatal("grace window is not bounded by the successor issue time")
	}
	record.AccessExpiresAt = mustInstant(t, now.Add(10*time.Second))
	if record.LiveAccessWithin(now.Add(15*time.Second), 30*time.Second) {
		t.Fatal("grace extended the token's own access expiry")
	}
	record.SupersededAt = value.Optional[temporal.DateTime]{}
	if !record.LiveAccessWithin(now.Add(5*time.Second), 0) {
		t.Fatal("current generation needs no grace")
	}
}
