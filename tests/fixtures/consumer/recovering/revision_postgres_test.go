package recovering_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
)

func TestRestoredEmailDoesNotRestoreRecoveryLinks(t *testing.T) {
	s := prepare(t)
	original := current(t, s)
	if original.EmailRevision.IsZero() {
		t.Fatal("creation omitted revision")
	}
	reset := issue(t, s)
	verify, err := s.verification.Issue(t.Context(), original.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	update(t, s, recovering.MemberDraft{}.SetEmail("changed@example.test").SetEmailVerified(true))
	changed := current(t, s)
	if changed.EmailRevision == original.EmailRevision || changed.EmailVerified {
		t.Fatal("email changed without fresh unverified state")
	}
	// Even explicitly submitting a stale revision cannot revive it.
	update(t, s, recovering.MemberDraft{}.SetEmail(original.Email).SetEmailRevision(original.EmailRevision))
	restored := current(t, s)
	if restored.EmailRevision == original.EmailRevision || restored.EmailRevision == changed.EmailRevision {
		t.Fatal("restoration reused a generation")
	}
	if _, err := s.reset.Complete(t.Context(), reset.Token(), plain(t, "replacement password")); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("restored email revived reset", err)
	}
	if _, err := s.verification.Complete(t.Context(), verify.Token()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("restored email revived verification", err)
	}
	freshReset := issue(t, s)
	freshVerify, err := s.verification.Issue(t.Context(), original.FoundryReference())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.reset.Complete(t.Context(), freshReset.Token(), plain(t, "replacement password")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.verification.Complete(t.Context(), freshVerify.Token()); err != nil {
		t.Fatal("password reset invalidated unchanged email", err)
	}
}

func TestUnchangedEmailAndRolledBackChangesPreserveRevision(t *testing.T) {
	s := prepare(t)
	original := current(t, s)
	link := issue(t, s)
	fabricated, err := challenge.NewRevision[recovering.Member]()
	if err != nil {
		t.Fatal(err)
	}
	update(t, s, recovering.MemberDraft{}.SetEmail(original.Email).SetEmailRevision(fabricated).SetEnabled(true))
	if current(t, s).EmailRevision != original.EmailRevision {
		t.Fatal("unchanged email replaced managed revision")
	}
	rollback := errors.New("rollback email mutation")
	err = s.within(t.Context(), func(tx *database.Tx) error {
		changed, err := recovering.QueryRecoveryMembers().Update(t.Context(), tx, original.ID, recovering.MemberDraft{}.SetEmail("rolled-back@example.test"))
		if err != nil {
			return err
		}
		if changed.EmailRevision == original.EmailRevision {
			return errors.New("mutation did not rotate revision")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if current(t, s).EmailRevision != original.EmailRevision {
		t.Fatal("rollback retained revision")
	}
	if _, err := s.reset.Complete(t.Context(), link.Token(), plain(t, "replacement password")); err != nil {
		t.Fatal("rollback invalidated live link", err)
	}
}

func TestRecoveryWaitsForEmailChangeThenRejectsOldRevision(t *testing.T) {
	s := prepare(t)
	link := issue(t, s)
	changed, release := make(chan struct{}), make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- s.within(t.Context(), func(tx *database.Tx) error {
			_, err := recovering.QueryRecoveryMembers().Update(t.Context(), tx, s.member.ID, recovering.MemberDraft{}.SetEmail("while-locked@example.test"))
			if err != nil {
				return err
			}
			close(changed)
			<-release
			return nil
		})
	}()
	select {
	case <-changed:
	case err := <-writeDone:
		t.Fatal("email write failed before locking", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	started, completed := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		_, err := s.reset.Complete(t.Context(), link.Token(), plain(t, "replacement password"))
		completed <- err
	}()
	<-started
	close(release)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-completed; !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("completion accepted previous generation", err)
	}
	if current(t, s).Password != s.member.Password {
		t.Fatal("stale reset changed password")
	}
}
