package multifactor_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type securityLog struct {
	bus       *events.Bus
	producer  *events.Outbox
	ids       []outbox.ID[multifactor.SecurityChange]
	delivered []multifactor.SecurityChange
	origins   []attribution.Origin
	reject    error
}

func securitySetup(t *testing.T, s *mfaFixture) *securityLog {
	t.Helper()
	l := &securityLog{}
	declaration, err := multifactor.SecurityChanged.Declare(events.Listen("security.fixture", func(ctx context.Context, event multifactor.SecurityChange) error {
		l.delivered = append(l.delivered, event)
		l.origins = append(l.origins, attribution.FromContext(ctx))
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	l.bus, err = events.Prepare(events.DefaultConfig(), declaration)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := l.bus.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	l.producer, err = events.PrepareOutbox("security.fixture", l.bus)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.within(t.Context(), func(tx *database.Tx) error {
		for _, definition := range outbox.Migrations() {
			for _, sql := range definition.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return l
}
func (l *securityLog) changed(ctx context.Context, tx *database.Tx, n multifactor.FactorNotice) error {
	id, err := multifactor.RecordSecurityChange(ctx, tx, l.producer, n)
	if err != nil {
		return err
	}
	l.ids = append(l.ids, id)
	if err := multifactor.SecurityChanged.AfterCommit(ctx, tx, l.bus, multifactor.SecurityChange{Account: n.Subject().Key(), Action: n.Action()}); err != nil {
		return err
	}
	return l.reject
}
func (l *securityLog) check(t *testing.T, s *mfaFixture, index int, present bool, action mfa.Action, origin attribution.Origin) {
	t.Helper()
	if err := s.within(t.Context(), func(tx *database.Tx) error {
		found, err := multifactor.SecurityChanged.Find(t.Context(), tx, l.producer, l.ids[index])
		if err != nil {
			return err
		}
		stored, ok := found.Get()
		if ok != present {
			return errors.New("outbox row did not share mutation outcome")
		}
		if ok {
			payload, err := stored.Payload()
			if err != nil {
				return err
			}
			if payload.Account != s.account.ID || payload.Action != action || stored.Origin() != origin {
				return errors.New("security payload or attribution changed")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresMFASecurityEventsShareEnrollmentAndCompletionTransactions(t *testing.T) {
	s := mfaSetup(t)
	l := securitySetup(t, s)
	var err error
	s.factors, err = s.factors.WithObserver(mfa.Observer[multifactor.Account, model.ID[multifactor.Account]]{Changed: l.changed})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := (attribution.Origin{}).WithSystem("fixture.enrollment")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	password := s.login(t)
	enrollment, err := s.factors.Enroll(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	l.reject = errors.New("reject security record")
	if _, err = s.factors.Confirm(ctx, password, enrollment.ID(), authenticatorCode(t, enrollment.Secret(), s.clock.Now())); !errors.Is(err, l.reject) {
		t.Fatal("observer error lost", err)
	}
	if s.current(t).MFAEnabled || len(l.delivered) != 0 || len(l.ids) != 1 {
		t.Fatal("failed enrollment escaped transaction")
	}
	l.check(t, s, 0, false, mfa.Enrolled, origin)
	l.reject = nil
	codes, err := s.factors.Confirm(ctx, password, enrollment.ID(), authenticatorCode(t, enrollment.Secret(), s.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.delivered) != 1 || l.origins[0] != origin {
		t.Fatal("after-commit enrollment did not capture initiator")
	}
	l.check(t, s, 1, true, mfa.Enrolled, origin)
	// An observer failure must preserve both the pending credential and factor
	// response for a later explicit retry; it may not return a full credential.
	kernel := s.kernel(t, "session")
	pending := pendingCredential(t, kernel)
	l.reject = errors.New("reject completion record")
	factor := verifier(t, s, codes.Codes()[0])
	if full, err := kernel.complete(t.Context(), pending, factor); !errors.Is(err, l.reject) || !full.IsZero() {
		t.Fatal("failed completion published authority", err)
	}
	if len(l.delivered) != 1 {
		t.Fatal("rolled-back completion dispatched")
	}
	l.check(t, s, 2, false, mfa.Verified, attribution.Origin{})
	l.reject = nil
	full, err := kernel.complete(t.Context(), pending, factor)
	if err != nil {
		t.Fatal(err)
	}
	if err := kernel.require(t, full); err != nil {
		t.Fatal(err)
	}
	l.check(t, s, 3, true, mfa.Verified, attribution.Origin{})
	if len(l.delivered) != 2 || l.delivered[1].Action != mfa.Verified {
		t.Fatal("completion event missing")
	}
}
func TestPostgresMFARetirementSharesOuterRollbackAndAcceptsDisabledAccounts(t *testing.T) {
	s := mfaSetup(t)
	s.enroll(t)
	s.issue(t, s.login(t))
	l := securitySetup(t, s)
	var err error
	s.factors, err = s.factors.WithObserver(mfa.Observer[multifactor.Account, model.ID[multifactor.Account]]{Changed: l.changed})
	if err != nil {
		t.Fatal(err)
	}
	// A disabled account can still be retired by an authorized administrator.
	if err := s.within(t.Context(), func(tx *database.Tx) error {
		_, err := multifactor.QueryMfaAccounts().Update(t.Context(), tx, s.account.ID, multifactor.AccountDraft{}.SetEnabled(false).SetRequireMFA(true))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("outer retirement rollback")
	if err := s.within(t.Context(), func(tx *database.Tx) error {
		if err := multifactor.RetireAccount(t.Context(), tx, s.factors, s.account); err != nil {
			return err
		}
		return rejected
	}); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	if !s.current(t).MFAEnabled || len(l.delivered) != 0 {
		t.Fatal("retirement escaped rollback")
	}
	s.countCredentials(t, 1)
	l.check(t, s, 0, false, mfa.Retired, attribution.Origin{})
	if err := s.within(t.Context(), func(tx *database.Tx) error { return multifactor.RetireAccount(t.Context(), tx, s.factors, s.account) }); err != nil {
		t.Fatal(err)
	}
	s.countCredentials(t, 0)
	l.check(t, s, 1, true, mfa.Retired, attribution.Origin{})
	if len(l.delivered) != 1 || l.delivered[0].Action != mfa.Retired {
		t.Fatal("retirement event missing")
	}
	if err := s.within(t.Context(), func(tx *database.Tx) error {
		found, err := multifactor.QueryMfaAccounts().Find(t.Context(), tx, s.account.ID)
		if err != nil {
			return err
		}
		if found.IsSet() {
			return errors.New("account not retired")
		}
		_, err = s.factors.RetireIn(t.Context(), tx, s.account.FoundryReference())
		if !errors.Is(err, auth.Unauthenticated) {
			return errors.New("missing account accepted for retirement")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresMFARejectionObservationCannotAuthorizeOrLeakSuccess(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			password := s.login(t)
			var changed, rejected int
			cause := errors.New("observer failure")
			observed, err := s.factors.WithObserver(mfa.Observer[multifactor.Account, model.ID[multifactor.Account]]{
				Changed: func(context.Context, *database.Tx, multifactor.FactorNotice) error { changed++; return nil },
				Rejected: func(_ context.Context, n multifactor.FactorNotice) error {
					rejected++
					if n.Subject().Key() != s.account.ID || n.Action() != mfa.Rejected {
						return errors.New("incorrect notice")
					}
					switch mode {
					case "panic":
						panic("observer panic")
					case "goexit":
						runtime.Goexit()
					}
					return cause
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := observed.WithObserver(mfa.Observer[multifactor.Account, model.ID[multifactor.Account]]{Changed: func(context.Context, *database.Tx, multifactor.FactorNotice) error { return nil }}); err == nil {
				t.Fatal("duplicate observer accepted")
			}
			// A consumed recovery code is guaranteed to mismatch; don't guess a TOTP.
			if _, err := s.factors.RegenerateRecovery(t.Context(), password, recoveryResponse(t, codes.Codes()[0])); err != nil {
				t.Fatal(err)
			}
			if result, err := observed.Disable(t.Context(), password, recoveryResponse(t, codes.Codes()[1])); !errors.Is(err, auth.Unauthenticated) || !result.ID.IsZero() {
				t.Fatal("observation changed denial", err)
			} else if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("observer cause lost")
			}
			if changed != 0 || rejected != 1 || !s.current(t).MFAEnabled {
				t.Fatal("denial changed protected state")
			}
			malformed, _ := mfa.ParseTOTPCode(secret.New("bad"))
			if _, err := observed.Confirm(t.Context(), password, mfa.EnrollmentID[multifactor.Account]{}, malformed); err == nil {
				t.Fatal("malformed confirmation accepted")
			}
			if rejected != 1 {
				t.Fatal("malformed input emitted verified account observation")
			}
		})
	}
}
