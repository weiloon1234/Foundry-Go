package postgres_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func rebind(t *testing.T, s *setup, adjust func(*token.Config)) *token.Tokens[member, int64] {
	t.Helper()
	config := s.config
	adjust(&config)
	store, err := token.NewStore(s.backend, config)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.New(store, "members.api", s.provider, "api.bearer", s.grants)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func scoped(t *testing.T, s *setup, tokens *token.Tokens[member, int64], raw secret.String) context.Context {
	t.Helper()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), tokens.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "api.bearer", Secret: raw})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Close() })
	if _, err := tokens.Guard().Require(scope.Context()); err != nil {
		t.Fatal(err)
	}
	return scope.Context()
}

func families(t *testing.T, s *setup) int64 {
	t.Helper()
	var n int64
	within(t, s, func(tx *database.Tx) error {
		var err error
		n, err = tokenstore.QueryFoundryTokenFamilies().Count(t.Context(), tx)
		return err
	})
	return n
}

// Challenge tokens have their own cap and never consume full capacity. Under
// EvictOldest a new family replaces the subject's oldest full family.
func TestTokenPendingCapAndEvictOldest(t *testing.T) {
	s := prepare(t)
	tokens := rebind(t, s, func(c *token.Config) { c.Limit = auth.EvictOldest; c.MaxPendingPerSubject = 1 })
	pending, err := auth.NewProof(member{ID: 7}.reference(), auth.PendingMFA)
	if err != nil {
		t.Fatal(err)
	}
	full, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	first, err := tokens.Issue(t.Context(), pending, token.IssueOptions[member]{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := tokens.Issue(t.Context(), pending, token.IssueOptions[member]{})
	if err != nil {
		t.Fatal(err)
	}
	issued := make([]token.Issued[member, int64], 3)
	for i := range issued {
		s.clock.Advance(time.Second)
		if issued[i], err = tokens.Issue(t.Context(), full, token.IssueOptions[member]{Scopes: s.grants}); err != nil {
			t.Fatal(err)
		}
	}
	if err := authenticate(t, s, issued[0].AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("oldest family was not evicted", err)
	}
	for _, kept := range issued[1:] {
		if err := authenticate(t, s, kept.AccessSecret()); err != nil {
			t.Fatal("recent family was evicted", err)
		}
	}
	if err := authenticate(t, s, first.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("replaced challenge still authenticates", err)
	}
	if err := authenticate(t, s, second.AccessSecret()); !errors.Is(err, auth.MFARequired) {
		t.Fatal("recent challenge was evicted by full issuance", err)
	}
	if n := families(t, s); n != 3 {
		t.Fatal("unexpected stored families", n)
	}
}

// Handlers revoke every other family while keeping the current one; device
// metadata captured at issuance is listed; pruning is bounded per batch.
func TestTokenRevokeOthersDeviceAndPruneExpired(t *testing.T) {
	s := prepare(t)
	origin, err := attribution.Origin{}.WithRequest(attribution.Request{IP: netip.MustParseAddr("198.51.100.4"), UserAgent: "CLI/3"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.tokens.Issue(ctx, proof, token.IssueOptions[member]{Scopes: s.grants, Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	other := issue(t, s, 7, false)
	listed, err := s.tokens.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(listed) != 2 {
		t.Fatal(err, len(listed))
	}
	for _, item := range listed {
		expected := auth.Device{}
		if item.ID() == current.Info().ID() {
			expected = auth.Device{ClientIP: netip.MustParseAddr("198.51.100.4"), UserAgent: "CLI/3"}
		}
		if item.Device() != expected {
			t.Fatal("device metadata was not persisted", item.Device())
		}
	}
	refreshed, err := s.tokens.Refresh(t.Context(), refreshSecret(t, current))
	if err != nil || refreshed.Info().Device() != current.Info().Device() {
		t.Fatal("refresh lost the family device", err)
	}
	request := scoped(t, s, s.tokens, refreshed.AccessSecret())
	if count, err := s.tokens.RevokeOthers(request); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := authenticate(t, s, other.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("other family survived", err)
	}
	if err := authenticate(t, s, refreshed.AccessSecret()); err != nil {
		t.Fatal("current family was revoked", err)
	}
	for _, id := range []int64{1, 2, 3, 4, 5} {
		issue(t, s, id, false)
	}
	s.clock.Advance(31 * time.Minute)
	if _, err := s.tokens.PruneExpired(t.Context(), 2, 1); err != nil {
		t.Fatal(err)
	}
	if n := families(t, s); n != 4 {
		t.Fatal("prune task exceeded its batch bound", n)
	}
	if _, err := s.tokens.PruneExpired(t.Context(), 2, 10); err != nil {
		t.Fatal(err)
	}
	if n := families(t, s); n != 0 {
		t.Fatal("prune task stopped early", n)
	}
}

// A logout right after a refresh presents the previous access token, which
// still authenticates during the grace: it must revoke the whole family.
func TestRevokeWithPreviousGenerationEndsTheFamily(t *testing.T) {
	s := prepare(t)
	first := issue(t, s, 7, true)
	rotated, err := s.tokens.Refresh(t.Context(), refreshSecret(t, first))
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticate(t, s, first.AccessSecret()); err != nil {
		t.Fatal("previous access token rejected within grace", err)
	}
	if removed, err := s.tokens.Revoke(t.Context(), first.AccessSecret()); err != nil || !removed {
		t.Fatal("logout with the previous access token did nothing", removed, err)
	}
	for _, raw := range []secret.String{first.AccessSecret(), rotated.AccessSecret()} {
		if err := authenticate(t, s, raw); !errors.Is(err, auth.Unauthenticated) {
			t.Fatal("family survived logout", err)
		}
	}
	if _, err := s.tokens.Refresh(t.Context(), refreshSecret(t, rotated)); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked family refreshed", err)
	}
}
