package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// A refresh-cookie logout needs no live access token: any refresh secret its
// family issued revokes the whole family, and an unknown one revokes nothing.
func TestRefreshLogoutRevokesTheFamilyOfAnyGeneration(t *testing.T) {
	s := prepare(t)
	var events []auth.Event
	tokens, err := s.tokens.WithObserver(func(_ context.Context, event auth.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	logout := func(raw secret.String) bool {
		t.Helper()
		removed, err := tokens.LogoutRefresh(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return removed
	}

	// The current generation, after its access token expired.
	current := issue(t, s, 7, true)
	unrelated := issue(t, s, 8, true)
	s.clock.Advance(s.config.Renewable.Access)
	if !logout(refreshSecret(t, current)) {
		t.Fatal("current refresh secret did not revoke its family")
	}
	identity, err := member{ID: 7}.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != auth.EventLogout {
		t.Fatal("refresh logout was not observed", events)
	}
	if subject, present := events[0].Subject.Get(); !present || subject != identity {
		t.Fatal("refresh logout observed another subject")
	}
	if _, err := s.tokens.Refresh(t.Context(), refreshSecret(t, current)); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked family refreshed", err)
	}
	if logout(refreshSecret(t, current)) || len(events) != 1 {
		t.Fatal("repeated logout was not idempotent")
	}
	if _, err := s.tokens.Refresh(t.Context(), refreshSecret(t, unrelated)); err != nil {
		t.Fatal("unrelated family revoked", err)
	}

	// The previous generation's secret, retained for replay detection.
	previous := issue(t, s, 9, true)
	rotated, err := s.tokens.Refresh(t.Context(), refreshSecret(t, previous))
	if err != nil {
		t.Fatal(err)
	}
	if !logout(refreshSecret(t, previous)) {
		t.Fatal("previous refresh secret did not revoke its family")
	}
	if err := authenticate(t, s, rotated.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("successor survived logout", err)
	}

	// A consumed secret, held only in the compact consumed set.
	consumed := issue(t, s, 10, true)
	second, err := s.tokens.Refresh(t.Context(), refreshSecret(t, consumed))
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.tokens.Refresh(t.Context(), refreshSecret(t, second))
	if err != nil {
		t.Fatal(err)
	}
	if !logout(refreshSecret(t, consumed)) {
		t.Fatal("consumed refresh secret did not revoke its family")
	}
	if err := authenticate(t, s, latest.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("latest generation survived logout", err)
	}

	// An access secret is no refresh secret, and a malformed one is rejected.
	live := issue(t, s, 11, false)
	if logout(live.AccessSecret()) {
		t.Fatal("access secret revoked as a refresh secret")
	}
	if err := authenticate(t, s, live.AccessSecret()); err != nil {
		t.Fatal("unknown refresh secret revoked a family", err)
	}
	if removed, err := tokens.LogoutRefresh(t.Context(), secret.New("not-a-token")); removed || !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("malformed refresh secret", removed, err)
	}

	// An expired family is removed too.
	expired := issue(t, s, 12, true)
	s.clock.Advance(s.config.Renewable.Absolute)
	if !logout(refreshSecret(t, expired)) || len(events) != 4 {
		t.Fatal("expired family was not removed", len(events))
	}
	within(t, s, func(tx *database.Tx) error {
		var families, consumedDigests int64
		if err := database.ScanOne(t.Context(), tx, `SELECT count(*) FROM foundry_token_families`, nil, &families); err != nil {
			return err
		}
		if err := database.ScanOne(t.Context(), tx, `SELECT count(*) FROM foundry_token_consumed_refreshes`, nil, &consumedDigests); err != nil {
			return err
		}
		// Only the unrelated and access-only families remain, with no consumed digests.
		if families != 2 || consumedDigests != 0 {
			t.Error("logout left family state behind", families, consumedDigests)
		}
		return nil
	})
}
