package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type resumptionBackend struct {
	*sessionpg.Backend
	beforeConsume func()
	afterConsume  func() error
}

// ResumeActor injects work before the resumption and inside its transaction,
// after the actor check, so tests can race revocation, rotation and expiry.
func (b *resumptionBackend) ResumeActor(ctx context.Context, address session.Address, expected session.Record, next session.Digest, check func(context.Context, *database.Tx) error) (value.Optional[session.Record], error) {
	if b.beforeConsume != nil {
		b.beforeConsume()
	}
	return b.Backend.ResumeActor(ctx, address, expected, next, func(op context.Context, tx *database.Tx) error {
		if err := check(op, tx); err != nil {
			return err
		}
		if b.afterConsume != nil {
			return b.afterConsume()
		}
		return nil
	})
}

func resumptionSessions(t *testing.T, s *setup, backend *resumptionBackend) *session.Sessions[member, int64] {
	t.Helper()
	store, err := session.NewStore(backend, s.config)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(store, "members.web", s.provider, "web.session")
	if err != nil {
		t.Fatal(err)
	}
	return sessions
}

func TestSessionResumeRevalidatesActorAndRollsBackConsumption(t *testing.T) {
	for _, mode := range []string{"revoked", "rotated", "expired", "creation failed", "expires before commit"} {
		t.Run(mode, func(t *testing.T) {
			s := prepare(t)
			backend := &resumptionBackend{Backend: s.backend}
			sessions := resumptionSessions(t, s, backend)
			impersonation, err := session.NewImpersonation(sessions, sessions, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			actor := issue(t, sessions, 1, auth.Authenticated, false)
			started, err := impersonation.Start(scoped(t, sessions, actor.Secret()), member{ID: 7}.reference(), time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			var injected error
			rejected := errors.New("creation rejected")
			backend.beforeConsume = func() {
				switch mode {
				case "revoked":
					_, injected = s.sessions.RevokeID(t.Context(), member{ID: 1}.reference(), actor.Info().ID())
				case "rotated":
					_, injected = sessions.Rotate(t.Context(), actor.Secret())
				case "expired":
					s.clock.Advance(11 * time.Minute)
				}
			}
			backend.afterConsume = func() error {
				if mode == "creation failed" {
					return rejected
				}
				if mode == "expires before commit" {
					s.clock.Advance(11 * time.Minute)
				}
				return nil
			}
			resumed, err := impersonation.Resume(scoped(t, sessions, started.Secret()))
			if injected != nil {
				t.Fatal(injected)
			}
			if err == nil || !resumed.Secret().IsZero() {
				t.Fatal("resume disclosed a session after actor validation failed")
			}
			if mode == "creation failed" {
				if !errors.Is(err, rejected) {
					t.Fatal("creation failure lost")
				}
			} else if !errors.Is(err, auth.Unauthenticated) || !errors.Is(err, session.ActorSessionEnded) {
				t.Fatal("actor end lost", err)
			}
			if _, err := authenticate(t, sessions, started.Secret()); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("failed resume kept impersonation alive")
			}
			rows := stored(t, s)
			want := 1
			if mode == "revoked" {
				want = 0
			}
			if len(rows) != want {
				t.Fatal("replacement escaped rollback", len(rows))
			}
			if mode == "creation failed" {
				if _, err := authenticate(t, sessions, actor.Secret()); err != nil {
					t.Fatal("failed creation consumed original actor", err)
				}
			}
		})
	}
}

func TestConcurrentSessionResumeHasOneWinner(t *testing.T) {
	s := prepare(t)
	backend := &resumptionBackend{Backend: s.backend}
	sessions := resumptionSessions(t, s, backend)
	impersonation, err := session.NewImpersonation(sessions, sessions, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	actor := issue(t, sessions, 1, auth.Authenticated, false)
	started, err := impersonation.Start(scoped(t, sessions, actor.Secret()), member{ID: 7}.reference(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, second := scoped(t, sessions, started.Secret()), scoped(t, sessions, started.Secret())
	// Admit both requests before either revokes their shared impersonation.
	if _, err := sessions.Current(first); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Current(second); err != nil {
		t.Fatal(err)
	}
	var arrived sync.WaitGroup
	arrived.Add(2)
	backend.beforeConsume = func() { arrived.Done(); arrived.Wait() }
	results := make(chan error, 2)
	for _, ctx := range []context.Context{first, second} {
		go func() { _, err := impersonation.Resume(ctx); results <- err }()
	}
	winners := 0
	for range 2 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, auth.Unauthenticated) {
			t.Fatal("unexpected resume error", err)
		}
	}
	if winners != 1 || len(stored(t, s)) != 1 {
		t.Fatal("actor credential was replaced more than once", winners)
	}
}

// An actor impersonates a member through a bounded, marked session. The
// original actor stays accessible, sensitive operations refuse the session, the
// member's own sessions are untouched, and Resume returns to a fresh actor session.
func TestSessionImpersonationStartResumeAndRefusals(t *testing.T) {
	s := prepare(t)
	var events []auth.Event
	base, err := session.NewImpersonation(s.sessions, s.sessions, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	impersonation, err := base.WithObserver(func(_ context.Context, event auth.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewImpersonation(s.sessions, s.sessions, 25*time.Hour); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded impersonation accepted", err)
	}
	own := []session.Issued[member, int64]{issue(t, s.sessions, 7, auth.Authenticated, false), issue(t, s.sessions, 7, auth.Authenticated, false)}
	admin := issue(t, s.sessions, 1, auth.Authenticated, true)
	adminCtx := scoped(t, s.sessions, admin.Secret())
	for _, invalid := range []struct {
		target   int64
		duration time.Duration
		want     error
	}{{1, time.Minute, fault.Invalid}, {7, 2 * time.Hour, fault.Invalid}, {7, 0, fault.Invalid}, {404, time.Minute, auth.Unauthenticated}} {
		if _, err := impersonation.Start(adminCtx, member{ID: invalid.target}.reference(), invalid.duration); !errors.Is(err, invalid.want) {
			t.Fatal("invalid impersonation accepted", invalid.target, err)
		}
	}
	started, err := impersonation.Start(adminCtx, member{ID: 7}.reference(), 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	info := started.Info()
	recorded, present := info.Impersonator().Get()
	adminIdentity, _ := member{ID: 1}.FoundryIdentity()
	if !info.Impersonated() || !present || recorded.Subject != adminIdentity || recorded.Session.String() != admin.Info().ID().String() || info.Remembered() || !info.ExpiresAt().UTC().Equal(info.CreatedAt().UTC().Add(30*time.Minute)) {
		t.Fatal("impersonation session is not marked or bounded")
	}
	for _, kept := range own {
		if _, err := authenticate(t, s.sessions, kept.Secret()); err != nil {
			t.Fatal("impersonation evicted the member's own session", err)
		}
	}
	targetCtx := scoped(t, s.sessions, started.Secret())
	actor, err := impersonation.Actor(targetCtx)
	if current, present := actor.Get(); err != nil || !present || current.ID != 1 {
		t.Fatal("original actor is not accessible", err)
	}
	if actor, err := impersonation.Actor(adminCtx); err != nil || actor.IsSet() {
		t.Fatal("an ordinary session reported an actor", err)
	}
	if err := s.sessions.RequireNotImpersonating(targetCtx); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("sensitive operation allowed while impersonating", err)
	}
	if _, err := s.sessions.ConfirmCurrent(targetCtx); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("impersonated session confirmed a password", err)
	}
	if err := s.sessions.RequireNotImpersonating(adminCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := impersonation.Start(targetCtx, member{ID: 8}.reference(), time.Minute); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("nested impersonation accepted", err)
	}
	// An impersonation session can act as its subject but never mint another
	// credential for it or log it out elsewhere.
	if _, err := s.sessions.CurrentProof(targetCtx); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("impersonation minted an unmarked proof", err)
	}
	if _, err := auth.CurrentProof(targetCtx, s.provider, s.sessions.Guard()); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("impersonation minted an unmarked proof through auth.CurrentProof", err)
	}
	if _, err := s.sessions.RevokeOthers(targetCtx); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("impersonation revoked the subject's other sessions", err)
	}
	for _, kept := range own {
		if _, err := authenticate(t, s.sessions, kept.Secret()); err != nil {
			t.Fatal("refused revocation removed a session", err)
		}
	}
	resumed, err := impersonation.Resume(targetCtx)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Info().Subject().Key() != 1 || resumed.Info().Impersonated() || !resumed.Info().Remembered() || resumed.Info().ID() != admin.Info().ID() || resumed.Info().CreatedAt() != admin.Info().CreatedAt() || resumed.Info().ExpiresAt() != admin.Info().ExpiresAt() {
		t.Fatal("resume did not restore the actor session in place")
	}
	// Repeated impersonation never extends the actor's absolute lifetime.
	for range 2 {
		s.clock.Advance(time.Minute)
		cycle, err := impersonation.Start(scoped(t, s.sessions, resumed.Secret()), member{ID: 7}.reference(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if resumed, err = impersonation.Resume(scoped(t, s.sessions, cycle.Secret())); err != nil {
			t.Fatal(err)
		}
		if resumed.Info().CreatedAt() != admin.Info().CreatedAt() || resumed.Info().ExpiresAt() != admin.Info().ExpiresAt() {
			t.Fatal("impersonation restarted the actor's absolute lifetime")
		}
	}
	for _, ended := range []session.Issued[member, int64]{started, admin} {
		if _, err := authenticate(t, s.sessions, ended.Secret()); !errors.Is(err, auth.Unauthenticated) {
			t.Fatal("impersonation or orphaned actor session survived resume", err)
		}
	}
	if got, err := authenticate(t, s.sessions, resumed.Secret()); err != nil || got.ID != 1 {
		t.Fatal("resumed actor session rejected", err)
	}

	// Revoking the actor's own session ends its impersonation immediately.
	resumedCtx := scoped(t, s.sessions, resumed.Secret())
	again, err := impersonation.Start(resumedCtx, member{ID: 7}.reference(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.sessions.RevokeID(t.Context(), member{ID: 1}.reference(), resumed.Info().ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(t, s.sessions, again.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("impersonation outlived its actor session", err)
	}

	// The bounded lifetime ends impersonation without any action.
	fresh := issue(t, s.sessions, 1, auth.Authenticated, false)
	bounded, err := impersonation.Start(scoped(t, s.sessions, fresh.Secret()), member{ID: 7}.reference(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(6 * time.Minute)
	if _, err := authenticate(t, s.sessions, bounded.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("impersonation outlived its duration", err)
	}
	kinds := make([]auth.EventKind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
		if impersonator, present := event.Impersonator.Get(); !present || impersonator != adminIdentity {
			t.Fatal("event lost the original actor", event.Kind)
		}
	}
	want := []auth.EventKind{auth.EventImpersonationStarted, auth.EventImpersonationStopped, auth.EventImpersonationStarted, auth.EventImpersonationStopped, auth.EventImpersonationStarted, auth.EventImpersonationStopped, auth.EventImpersonationStarted, auth.EventImpersonationStarted}
	if len(kinds) != len(want) {
		t.Fatal("unexpected impersonation events", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatal("unexpected impersonation events", kinds)
		}
	}
}

// Revoking every actor session (logout everywhere, password reset joining the
// credential-change transaction) or its expiry ends the impersonations it
// started. Events emitted from an impersonation session name the actor.
func TestImpersonationEndsWithTheActorsSessions(t *testing.T) {
	s := prepare(t)
	var events []auth.Event
	observed, err := s.sessions.WithObserver(func(_ context.Context, event auth.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	impersonation, err := session.NewImpersonation(observed, observed, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other, err := session.New(s.store, "members.other", s.provider, "other.session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewImpersonation(observed, other, time.Hour); err != nil {
		t.Fatal("same-store bindings rejected", err)
	}
	start := func() session.Issued[member, int64] {
		t.Helper()
		actor := issue(t, observed, 1, auth.Authenticated, false)
		started, err := impersonation.Start(scoped(t, observed, actor.Secret()), member{ID: 7}.reference(), 30*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authenticate(t, observed, started.Secret()); err != nil {
			t.Fatal(err)
		}
		return started
	}
	adminIdentity, _ := member{ID: 1}.FoundryIdentity()

	// A logout from the impersonation session names the actor.
	logout := start()
	if removed, err := observed.Logout(scoped(t, observed, logout.Secret()), logout.Secret()); err != nil || !removed {
		t.Fatal(removed, err)
	}
	last := events[len(events)-1]
	if impersonator, present := last.Impersonator.Get(); last.Kind != auth.EventLogout || !present || impersonator != adminIdentity {
		t.Fatal("logout event lost the impersonating actor", last.Kind)
	}

	revoked := start()
	if _, err := observed.RevokeAll(t.Context(), member{ID: 1}.reference()); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(t, observed, revoked.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoke-all of the actor left its impersonation alive", err)
	}

	reset := start()
	if err := s.db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := observed.RevokeAllIn(t.Context(), tx, member{ID: 1}.reference())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(t, observed, reset.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("a password reset of the actor left its impersonation alive", err)
	}
	listed, err := observed.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(listed) != 0 {
		t.Fatal("ended impersonations are still listed", len(listed), err)
	}

	// The impersonation stays active while the actor's own session idles out
	// (10 minutes); it ends with the actor's session.
	expiring := start()
	s.clock.Advance(6 * time.Minute)
	if _, err := authenticate(t, observed, expiring.Secret()); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(5 * time.Minute)
	if _, err := authenticate(t, observed, expiring.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("impersonation outlived its actor's session", err)
	}
}

// A sibling store cannot back an impersonation: the actor check needs the
// actor's session in the same store.
func TestImpersonationRequiresOneStore(t *testing.T) {
	s := prepare(t)
	separate, err := session.NewStore(s.backend, s.config)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := session.New(separate, "members.other", s.provider, "other.session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewImpersonation(s.sessions, targets, time.Hour); !errors.Is(err, fault.Invalid) {
		t.Fatal("impersonation across stores accepted", err)
	}
}
