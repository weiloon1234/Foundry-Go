package postgres_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// scoped returns a live auth scope authenticated by credential.
func scoped(t *testing.T, sessions *session.Sessions[member, int64], credential secret.String) context.Context {
	t.Helper()
	guard := sessions.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: guard.Source(), Secret: credential})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Close() })
	if _, err := guard.Require(scope.Context()); err != nil {
		t.Fatal(err)
	}
	return scope.Context()
}

// Pending-MFA sessions have their own cap and never consume full-session
// capacity; a new pending session replaces the oldest pending one.
func TestSessionPendingMFAUsesSeparateCap(t *testing.T) {
	s := prepare(t)
	config := s.config
	config.Limit = auth.RejectNew
	config.MaxPendingPerSubject = 2
	store, err := session.NewStore(s.backend, config)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(store, "members.web", s.provider, "web.session")
	if err != nil {
		t.Fatal(err)
	}
	pending := make([]session.Issued[member, int64], 3)
	for i := range pending {
		pending[i] = issue(t, sessions, 7, auth.PendingMFA, false)
	}
	if _, err := authenticate(t, sessions, pending[0].Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("oldest pending session was not replaced", err)
	}
	for _, kept := range pending[1:] {
		if _, err := authenticate(t, sessions, kept.Secret()); !errors.Is(err, auth.MFARequired) {
			t.Fatal("recent pending session was evicted", err)
		}
	}
	issue(t, sessions, 7, auth.Authenticated, false)
	issue(t, sessions, 7, auth.Authenticated, false)
	proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Issue(t.Context(), proof, session.IssueOptions{}); !errors.Is(err, auth.CredentialLimit) {
		t.Fatal("full cap counted pending sessions or was not enforced", err)
	}
	if len(stored(t, s)) != 4 {
		t.Fatal("unexpected stored sessions", len(stored(t, s)))
	}
}

// Sliding activity is written at most once per TouchInterval, so ordinary
// requests do not update the session row every time.
func TestSessionTouchIsThrottled(t *testing.T) {
	s := prepare(t)
	issued := issue(t, s.sessions, 7, auth.Authenticated, false)
	created := stored(t, s)[0].LastSeenAt
	s.clock.Advance(30 * time.Second)
	if _, err := authenticate(t, s.sessions, issued.Secret()); err != nil {
		t.Fatal(err)
	}
	if got := stored(t, s)[0].LastSeenAt; got != created {
		t.Fatal("activity written before the touch interval")
	}
	s.clock.Advance(time.Minute)
	if _, err := authenticate(t, s.sessions, issued.Secret()); err != nil {
		t.Fatal(err)
	}
	row := stored(t, s)[0]
	if !row.LastSeenAt.UTC().Equal(created.UTC().Add(90*time.Second)) || !row.IdleExpiresAt.UTC().Equal(created.UTC().Add(90*time.Second+10*time.Minute)) {
		t.Fatal("activity was not recorded after the touch interval")
	}
}

// Handlers read the verified session, keep it while revoking the subject's
// other sessions, confirm the password in it and require a recent confirmation.
// Device metadata captured at issuance is listed; lifecycle events are observed.
func TestSessionCurrentOperationsDeviceAndConfirmation(t *testing.T) {
	s := prepare(t)
	var events []auth.Event
	sessions, err := s.sessions.WithObserver(func(_ context.Context, event auth.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	origin, err := attribution.Origin{}.WithRequest(attribution.Request{IP: netip.MustParseAddr("2001:db8::7"), UserAgent: "Browser/2"})
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
	current, err := sessions.Issue(ctx, proof, session.IssueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other := issue(t, sessions, 7, auth.Authenticated, false)
	listed, err := sessions.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(listed) != 2 {
		t.Fatal(err)
	}
	for _, item := range listed {
		expected := auth.Device{}
		if item.ID() == current.Info().ID() {
			expected = auth.Device{ClientIP: netip.MustParseAddr("2001:db8::7"), UserAgent: "Browser/2"}
		}
		if item.Device() != expected {
			t.Fatal("device metadata was not persisted", item.Device())
		}
	}
	request := scoped(t, sessions, current.Secret())
	info, err := sessions.Current(request)
	if err != nil || info.ID() != current.Info().ID() || info.ConfirmedAt().IsSet() {
		t.Fatal("current session metadata", err)
	}
	if err := sessions.RequireConfirmed(request, 5*time.Minute); !errors.Is(err, auth.ConfirmationRequired) {
		t.Fatal("unconfirmed session passed", err)
	}
	confirmed, err := sessions.ConfirmCurrent(request)
	if err != nil || !confirmed.ConfirmedAt().IsSet() {
		t.Fatal("confirmation was not recorded", err)
	}
	fresh := scoped(t, sessions, current.Secret())
	if err := sessions.RequireConfirmed(fresh, 5*time.Minute); err != nil {
		t.Fatal("recent confirmation rejected", err)
	}
	s.clock.Advance(6 * time.Minute)
	later := scoped(t, sessions, current.Secret())
	if err := sessions.RequireConfirmed(later, 5*time.Minute); !errors.Is(err, auth.ConfirmationRequired) {
		t.Fatal("stale confirmation accepted", err)
	}
	if count, err := sessions.RevokeOthers(later); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := authenticate(t, sessions, other.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("other session survived", err)
	}
	if removed, err := sessions.RevokeCurrent(later); err != nil || !removed {
		t.Fatal("current session was not revoked", removed, err)
	}
	if _, err := authenticate(t, sessions, current.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked current session authenticated", err)
	}
	kinds := make([]auth.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	if len(events) != 4 || kinds[0] != auth.EventLogin || kinds[2] != auth.EventOtherDevicesLoggedOut || events[2].Count != 1 || kinds[3] != auth.EventLogout || events[0].Request.IP != netip.MustParseAddr("2001:db8::7") {
		t.Fatal("unexpected lifecycle events", kinds)
	}
}

// The prune task keeps deleting bounded batches until a short batch.
func TestSessionPruneExpiredDrainsBatches(t *testing.T) {
	s := prepare(t)
	for _, id := range []int64{1, 2, 3, 4, 5} {
		issue(t, s.sessions, id, auth.Authenticated, false)
	}
	s.clock.Advance(21 * time.Minute)
	if _, err := s.sessions.PruneExpired(t.Context(), 2, 1); err != nil {
		t.Fatal(err)
	}
	if len(stored(t, s)) != 3 {
		t.Fatal("prune task exceeded its batch bound")
	}
	if _, err := s.sessions.PruneExpired(t.Context(), 2, 10); err != nil {
		t.Fatal(err)
	}
	if len(stored(t, s)) != 0 {
		t.Fatal("prune task stopped early")
	}
	if _, err := s.sessions.PruneExpired(t.Context(), 0, 1); err == nil {
		t.Fatal("invalid prune bounds accepted")
	}
}
