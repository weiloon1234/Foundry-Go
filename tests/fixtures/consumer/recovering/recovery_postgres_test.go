package recovering_test

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	challengepg "github.com/weiloon1234/Foundry-Go/auth/challenge/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/emailverification"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/passwordreset"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fixture struct {
	provider      recovering.Provider
	namespace     keyspace.Namespace
	db            *database.DB
	schema        string
	clock         *testkit.Clock
	member        recovering.Member
	hasher        *password.Hasher
	reset         *recovering.Reset
	verification  *recovering.Verification
	invalidations atomic.Int32
	invalidate    func(context.Context, *database.Tx, recovering.Member) error
}

func (s *fixture) within(ctx context.Context, fn func(*database.Tx) error) error {
	return s.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	})
}
func prepare(t *testing.T) *fixture {
	t.Helper()
	s := &fixture{db: pgtest.Open(t), clock: testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))}
	s.schema = pgtest.Namespace(t, s.db)
	s.namespace = keyspace.Namespace{Application: "foundry-recovery-tests", Environment: "test"}
	if _, err := migrate.New(challengepg.Migrations()...); err != nil {
		t.Fatal(err)
	}
	config := password.DefaultConfig()
	config.Parameters = password.Parameters{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
	var err error
	s.hasher, err = password.New(config)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.hasher.Hash(t.Context(), plain(t, "old long password"))
	if err != nil {
		t.Fatal(err)
	}
	err = s.within(t.Context(), func(tx *database.Tx) error {
		for _, definition := range challengepg.Migrations() {
			for _, sql := range definition.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(t.Context(), `CREATE TABLE recovery_members(id uuid PRIMARY KEY,email text NOT NULL,email_revision uuid NOT NULL,password text NOT NULL,email_verified boolean NOT NULL,enabled boolean NOT NULL)`); err != nil {
			return err
		}
		s.member, err = recovering.QueryRecoveryMembers().Create(t.Context(), tx, recovering.MemberDraft{}.SetEmail("member@example.test").SetPassword(old).SetEmailVerified(false).SetEnabled(true))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := challengepg.New(s.db, challengepg.Config{Schema: s.schema, Clock: s.clock})
	if err != nil {
		t.Fatal(err)
	}
	store, err := challenge.NewStore(backend, challenge.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	// Test schemas use a scoped provider lookup so ordinary guards can resolve
	// current models. Recovery itself continues using the supplied tx lock.
	provider := auth.DefineProvider("recovery.members", (recovering.Member{}).FoundryReference(), func(ctx context.Context, id model.ID[recovering.Member]) (value.Optional[recovering.Member], error) {
		var found value.Optional[recovering.Member]
		err := s.within(ctx, func(tx *database.Tx) error {
			var err error
			found, err = recovering.QueryRecoveryMembers().Find(ctx, tx, id)
			return err
		})
		return found, err
	}, func(_ context.Context, member recovering.Member) (bool, error) { return member.Enabled, nil })
	s.provider = provider
	s.reset, err = recovering.NewReset(store, provider, s.hasher, func(ctx context.Context, tx *database.Tx, member recovering.Member) error {
		s.invalidations.Add(1)
		if tx == nil || member.ID != s.member.ID {
			return errors.New("wrong transactional invalidation target")
		}
		// Test spy only: real session/token invalidation integration has separate
		// acceptance requirements. This fixture proves callback/rollback ownership.
		if s.invalidate != nil {
			return s.invalidate(ctx, tx, member)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.verification, err = recovering.NewVerification(store, provider)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func plain(t *testing.T, raw string) password.Plaintext {
	t.Helper()
	p, err := password.NewPlaintext(secret.New(raw))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func current(t *testing.T, s *fixture) recovering.Member {
	t.Helper()
	var result recovering.Member
	err := s.within(t.Context(), func(tx *database.Tx) error {
		var err error
		result, err = recovering.QueryRecoveryMembers().RequireFind(t.Context(), tx, s.member.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func update(t *testing.T, s *fixture, draft recovering.MemberDraft) {
	t.Helper()
	if err := s.within(t.Context(), func(tx *database.Tx) error {
		_, err := recovering.QueryRecoveryMembers().Update(t.Context(), tx, s.member.ID, draft)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func issue(t *testing.T, s *fixture) passwordreset.Issued[recovering.Member] {
	t.Helper()
	issued, err := s.reset.Issue(t.Context(), s.member.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	return issued
}
func TestResetAndVerificationOwnPurposeAndCurrentModel(t *testing.T) {
	s := prepare(t)
	first := issue(t, s)
	second := issue(t, s)
	next := plain(t, "new long password")
	if _, err := s.reset.Complete(t.Context(), first.Token(), next); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("replaced reset token accepted", err)
	}
	verify, err := s.verification.Issue(t.Context(), s.member.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately reparse at the erased transport boundary: stored purpose still
	// rejects crossing even when a caller explicitly reconstructs a typed token.
	wrong, err := passwordreset.ParseToken[recovering.Member](verify.Token().Secret())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.reset.Complete(t.Context(), wrong, next); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("verification credential reset password", err)
	}
	changed, err := s.reset.Complete(t.Context(), second.Token(), next)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Password == s.member.Password || s.invalidations.Load() != 1 {
		t.Fatal("reset omitted password/invalidation")
	}
	if ok, err := s.hasher.Check(t.Context(), next, current(t, s).Password); err != nil || !ok {
		t.Fatal("password not stored", err)
	}
	if _, err := s.reset.Complete(t.Context(), second.Token(), next); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("reset token reused", err)
	}
	verified, err := s.verification.Complete(t.Context(), verify.Token())
	if err != nil || !verified.EmailVerified {
		t.Fatal("email verification failed after independent password change", err)
	}
	if _, err := s.verification.Complete(t.Context(), verify.Token()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("verification reused", err)
	}
	if _, err := s.verification.Issue(t.Context(), s.member.FoundryReference()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("verified email issued another link", err)
	}
}
func TestResetFailureRollsBackPasswordAndLeavesLinkUsable(t *testing.T) {
	for _, failure := range []string{"error", "panic", "goexit", "cancel", "expiry"} {
		t.Run(failure, func(t *testing.T) {
			s := prepare(t)
			issued := issue(t, s)
			next := plain(t, "replacement password")
			cause := errors.New("invalidation failure")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.invalidate = func(context.Context, *database.Tx, recovering.Member) error {
				switch failure {
				case "error":
					return cause
				case "panic":
					panic("private recovery details")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					cancel()
				case "expiry":
					s.clock.Advance(passwordreset.DefaultLifetime)
				}
				return nil
			}
			result, err := s.reset.Complete(ctx, issued.Token(), next)
			if err == nil || !result.ID.IsZero() {
				t.Fatal("failure published result")
			}
			if failure == "error" && !errors.Is(err, cause) {
				t.Fatal("error identity lost", err)
			}
			if current(t, s).Password != s.member.Password {
				t.Fatal("failed reset committed password")
			}
			s.invalidate = nil
			if failure == "expiry" {
				if _, err := s.reset.Complete(t.Context(), issued.Token(), next); !errors.Is(err, auth.Unauthenticated) {
					t.Fatal("expired token accepted", err)
				}
				if count, err := s.reset.Prune(t.Context(), 1); err != nil || count != 1 {
					t.Fatal("expired token not retained for pruning", err)
				}
			} else {
				if _, err := s.reset.Complete(t.Context(), issued.Token(), next); err != nil {
					t.Fatal("rollback consumed token", err)
				}
			}
		})
	}
}
func TestRecoveryRejectsChangedEmailPasswordAndDisabledModels(t *testing.T) {
	for _, change := range []string{"email", "password", "disabled"} {
		t.Run(change, func(t *testing.T) {
			s := prepare(t)
			reset := issue(t, s)
			verification, err := s.verification.Issue(t.Context(), s.member.FoundryReference())
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "email":
				update(t, s, recovering.MemberDraft{}.SetEmail("changed@example.test"))
			case "password":
				hash, err := s.hasher.Hash(t.Context(), plain(t, "other long password"))
				if err != nil {
					t.Fatal(err)
				}
				update(t, s, recovering.MemberDraft{}.SetPassword(hash))
			case "disabled":
				update(t, s, recovering.MemberDraft{}.SetEnabled(false))
			}
			if _, err := s.reset.Complete(t.Context(), reset.Token(), plain(t, "new long password")); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("stale reset accepted", err)
			}
			if change != "password" {
				if _, err := s.verification.Complete(t.Context(), verification.Token()); !errors.Is(err, auth.Unauthenticated) {
					t.Fatal("stale verification accepted", err)
				}
			}
			if s.invalidations.Load() != 0 {
				t.Fatal("invalid link reached invalidation")
			}
		})
	}
}
func TestConcurrentResetHasOneConsumer(t *testing.T) {
	s := prepare(t)
	issued := issue(t, s)
	next := plain(t, "new long password")
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := s.reset.Complete(t.Context(), issued.Token(), next); results <- err }()
	}
	close(start)
	success, denied := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, auth.Unauthenticated) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || denied != 1 || s.invalidations.Load() != 1 {
		t.Fatal("single use failed", success, denied, s.invalidations.Load())
	}
}
func TestRevocationAndPruningSeparatePurposeAndBoundWork(t *testing.T) {
	s := prepare(t)
	reset := issue(t, s)
	verify, err := s.verification.Issue(t.Context(), s.member.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := s.reset.Revoke(t.Context(), s.member.FoundryReference()); err != nil || !removed {
		t.Fatal("revoke", err)
	}
	if _, err := s.reset.Complete(t.Context(), reset.Token(), plain(t, "new long password")); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked link accepted", err)
	}
	if _, err := s.verification.Complete(t.Context(), verify.Token()); err != nil {
		t.Fatal("reset revoke affected verification", err)
	}
	update(t, s, recovering.MemberDraft{}.SetEmailVerified(false))
	_ = issue(t, s)
	if _, err := s.verification.Issue(t.Context(), s.member.FoundryReference()); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(passwordreset.DefaultLifetime)
	if count, err := s.reset.Prune(t.Context(), 1); err != nil || count != 1 {
		t.Fatal("reset prune", err)
	}
	if count, err := s.verification.Prune(t.Context(), 1); err != nil || count != 0 {
		t.Fatal("live verification pruned", err)
	}
	s.clock.Advance(emailverification.DefaultLifetime)
	if count, err := s.verification.Prune(t.Context(), 1); err != nil || count != 1 {
		t.Fatal("verification prune", err)
	}
}

func TestAfterCommitFailureRetainsCommittedRecoveryOutcome(t *testing.T) {
	s := prepare(t)
	issued := issue(t, s)
	next := plain(t, "new long password")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("after commit failed")
	s.invalidate = func(_ context.Context, tx *database.Tx, _ recovering.Member) error {
		return tx.AfterCommit(func(context.Context) error { cancel(); return cause })
	}
	result, err := s.reset.Complete(ctx, issued.Token(), next)
	var databaseError *database.Error
	if err == nil || !result.ID.IsZero() || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !errors.As(err, &databaseError) || databaseError.Outcome() != database.Committed {
		t.Fatal("lost committed outcome", err)
	}
	if current(t, s).Password == s.member.Password {
		t.Fatal("committed password was lost")
	}
	if _, err := s.reset.Complete(t.Context(), issued.Token(), next); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("committed token consumed twice", err)
	}
}
