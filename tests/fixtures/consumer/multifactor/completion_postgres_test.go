package multifactor_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type accountFactor = auth.SecondFactor[multifactor.Account, model.ID[multifactor.Account]]
type completionKernel struct {
	issue    func() (secret.String, error)
	complete func(context.Context, secret.String, accountFactor) (secret.String, error)
	guard    auth.Guard[multifactor.Account]
	source   auth.CredentialName
}

func (s *mfaFixture) kernel(t *testing.T, kind string) completionKernel {
	t.Helper()
	password := s.login(t)
	if kind == "session" {
		return completionKernel{
			issue: func() (secret.String, error) {
				issued, err := s.sessions.Issue(t.Context(), password.Proof(), session.IssueOptions{})
				return issued.Secret(), err
			},
			complete: func(ctx context.Context, pending secret.String, factor accountFactor) (secret.String, error) {
				full, err := s.sessions.CompleteMFA(ctx, pending, factor, session.IssueOptions{Remember: true})
				if err == nil && (full.Info().Assurance() != auth.Authenticated || !full.Info().Remembered() || full.Info().Subject().Key() != s.account.ID) {
					return secret.String{}, errors.New("incorrect full session metadata")
				}
				return full.Secret(), err
			}, guard: s.sessions.Guard(), source: "mfa.session",
		}
	}
	return completionKernel{
		issue: func() (secret.String, error) {
			issued, err := s.tokens.Issue(t.Context(), password.Proof(), token.IssueOptions[multifactor.Account]{Name: "MFA challenge"})
			return issued.AccessSecret(), err
		},
		complete: func(ctx context.Context, pending secret.String, factor accountFactor) (secret.String, error) {
			full, err := s.tokens.CompleteMFA(ctx, pending, factor, token.IssueOptions[multifactor.Account]{Name: "Full API", Refresh: true})
			if err == nil && (full.Info().Assurance() != auth.Authenticated || full.Info().Mode() != token.Renewable || !full.RefreshSecret().IsSet() || full.Info().Subject().Key() != s.account.ID) {
				return secret.String{}, errors.New("incorrect full token metadata")
			}
			return full.AccessSecret(), err
		}, guard: s.tokens.Guard(), source: "mfa.bearer",
	}
}
func (k completionKernel) require(t *testing.T, raw secret.String) error {
	t.Helper()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), k.guard.Registration())
	if err != nil {
		return err
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: k.source, Secret: raw})
	if err != nil {
		return err
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		return err
	}
	defer scope.Close()
	_, err = k.guard.Require(scope.Context())
	return err
}
func pendingCredential(t *testing.T, k completionKernel) secret.String {
	t.Helper()
	raw, err := k.issue()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func verifier(t *testing.T, s *mfaFixture, code mfa.RecoveryCode) accountFactor {
	t.Helper()
	v, err := s.factors.Verifier(recoveryResponse(t, code))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPostgresMFACompletionReturnsFreshGuardedCredentialsWithOneModelRead(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, kind)
			pending := pendingCredential(t, kernel)
			if err := kernel.require(t, pending); !errors.Is(err, auth.MFARequired) {
				t.Fatal("pending credential authorized", err)
			}
			var modelReads atomic.Int32
			binding := multifactor.FactorModel(s.invalidate)
			lock := binding.Lock
			binding.Lock = func(ctx context.Context, tx *database.Tx, id model.ID[multifactor.Account]) (value.Optional[multifactor.Account], error) {
				modelReads.Add(1)
				return lock(ctx, tx, id)
			}
			var err error
			s.factors, err = mfa.New(s.store, s.provider, binding, s.attempts)
			if err != nil {
				t.Fatal(err)
			}
			s.providerLookups.Store(0)
			full, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[0]))
			if err != nil {
				t.Fatal(err)
			}
			if full.IsZero() || full == pending || modelReads.Load() != 1 || s.providerLookups.Load() != 0 {
				t.Fatal("completion reused secret or hydrated twice")
			}
			if err := kernel.require(t, full); err != nil {
				t.Fatal("full credential did not authorize", err)
			}
			if err := kernel.require(t, pending); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("consumed pending credential still exists", err)
			}
			if raw, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[1])); !errors.Is(err, auth.Unauthenticated) || !raw.IsZero() {
				t.Fatal("pending credential completed twice", err)
			}
			if raw, err := kernel.complete(t.Context(), full, verifier(t, s, codes.Codes()[1])); !errors.Is(err, auth.Unauthenticated) || !raw.IsZero() {
				t.Fatal("full credential promoted again", err)
			}
			next := pendingCredential(t, kernel)
			if _, err := kernel.complete(t.Context(), next, verifier(t, s, codes.Codes()[1])); err != nil {
				t.Fatal("rejected replay consumed unrelated recovery code", err)
			}
		})
	}
}

func TestPostgresConcurrentMFACompletionConsumesOnlyWinningFactor(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, kind)
			pending := pendingCredential(t, kernel)
			type outcome struct {
				index int
				raw   secret.String
				err   error
			}
			outcomes := make(chan outcome, 2)
			start := make(chan struct{})
			checks := []accountFactor{verifier(t, s, codes.Codes()[0]), verifier(t, s, codes.Codes()[1])}
			for i := range 2 {
				go func() {
					<-start
					raw, err := kernel.complete(t.Context(), pending, checks[i])
					outcomes <- outcome{i, raw, err}
				}()
			}
			close(start)
			success, failed, loser := 0, 0, -1
			for range 2 {
				got := <-outcomes
				if got.err == nil && !got.raw.IsZero() {
					success++
				} else if errors.Is(got.err, auth.Unauthenticated) && got.raw.IsZero() {
					failed++
					loser = got.index
				} else {
					t.Fatal("unexpected concurrent completion", got.err)
				}
			}
			if success != 1 || failed != 1 {
				t.Fatal("challenge completed more than once")
			}
			next := pendingCredential(t, kernel)
			if _, err := kernel.complete(t.Context(), next, checks[loser]); err != nil {
				t.Fatal("losing transaction consumed its recovery factor", err)
			}
		})
	}
}

type completionBackend struct {
	*mfapg.Backend
	after func(context.Context, *database.Tx) error
}

func (b *completionBackend) WithinIn(ctx context.Context, tx *database.Tx, address mfa.Address, identity model.Identity, prepare func(context.Context, *database.Tx) error, change func(context.Context, *database.Tx, value.Optional[mfa.Record], temporal.DateTime) (mfa.Change, error)) (value.Optional[mfa.Record], error) {
	result, err := b.Backend.WithinIn(ctx, tx, address, identity, prepare, change)
	if err != nil {
		return value.Optional[mfa.Record]{}, err
	}
	if b.after != nil {
		if err := b.after(ctx, tx); err != nil {
			return value.Optional[mfa.Record]{}, err
		}
	}
	return result, nil
}
func (s *mfaFixture) completionBackend(t *testing.T, backend mfa.Backend) {
	t.Helper()
	keys, err := encryption.NewKeyring(s.key.ID(), s.key)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mfa.NewStore(backend, keys, mfa.DefaultConfig(s.namespace, "Foundry test"))
	if err != nil {
		t.Fatal(err)
	}
	s.factors, err = multifactor.NewFactors(store, s.provider, s.attempts, s.invalidate)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresMFACompletionRollsBackConsumptionAndFactorOnLateFailure(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		for _, mode := range []string{"adapter-error", "cancel", "late-lockout", "late-expiry"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				s := mfaSetup(t)
				_, codes := s.enroll(t)
				kernel := s.kernel(t, kind)
				pending := pendingCredential(t, kernel)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cause := errors.New("late factor adapter failure")
				wrapper := &completionBackend{Backend: s.backend}
				switch mode {
				case "adapter-error":
					wrapper.after = func(context.Context, *database.Tx) error { return cause }
				case "cancel":
					wrapper.after = func(context.Context, *database.Tx) error { cancel(); return nil }
				case "late-lockout":
					s.locks.reject.Store(true)
				case "late-expiry":
					s.locks.afterSuccess = func() { s.clock.Advance(16 * time.Minute) }
				}
				s.completionBackend(t, wrapper)
				check := verifier(t, s, codes.Codes()[0])
				raw, err := kernel.complete(ctx, pending, check)
				if err == nil || !raw.IsZero() {
					t.Fatal("failed completion published credential")
				}
				if mode == "adapter-error" && !errors.Is(err, cause) {
					t.Fatal("adapter error lost", err)
				}
				wrapper.after = nil
				s.locks.reject.Store(false)
				s.locks.afterSuccess = nil
				if mode == "late-expiry" {
					if err := kernel.require(t, pending); !errors.Is(err, auth.Unauthenticated) {
						t.Fatal("expired pending remains valid", err)
					}
					pending = pendingCredential(t, kernel)
				} else if err := kernel.require(t, pending); !errors.Is(err, auth.MFARequired) {
					t.Fatal("failed completion consumed pending credential", err)
				}
				// The exact recovery code is still available, even after its factor write
				// completed inside a savepoint before the outer creation failed.
				if _, err := kernel.complete(t.Context(), pending, check); err != nil {
					t.Fatal("rollback lost recovery factor", err)
				}
			})
		}
	}
}

func TestPostgresMFACompletionChecksDisabledModelsAndProviderIdentity(t *testing.T) {
	s := mfaSetup(t)
	_, codes := s.enroll(t)
	kernel := s.kernel(t, "token")
	pending := pendingCredential(t, kernel)
	check := verifier(t, s, codes.Codes()[0])
	otherProvider := multifactor.Accounts(s.db)
	other, err := multifactor.NewFactors(s.store, otherProvider, s.attempts, s.invalidate)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := other.Verifier(recoveryResponse(t, codes.Codes()[0]))
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := kernel.complete(t.Context(), pending, wrong); err == nil || !raw.IsZero() {
		t.Fatal("different provider declaration accepted")
	}
	err = s.within(t.Context(), func(tx *database.Tx) error {
		_, err := multifactor.QueryMfaAccounts().Update(t.Context(), tx, s.account.ID, multifactor.AccountDraft{}.SetEnabled(false))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := kernel.complete(t.Context(), pending, check); !errors.Is(err, auth.Unauthenticated) || !raw.IsZero() {
		t.Fatal("disabled model completed MFA", err)
	}
	err = s.within(t.Context(), func(tx *database.Tx) error {
		_, err := multifactor.QueryMfaAccounts().Update(t.Context(), tx, s.account.ID, multifactor.AccountDraft{}.SetEnabled(true))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.complete(t.Context(), pending, check); err != nil {
		t.Fatal("failed eligibility consumed credential/factor", err)
	}
}

func TestPostgresMFACompletionPreservesGuardAndScopeBoundaries(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, kind)
			pending := pendingCredential(t, kernel)
			check := verifier(t, s, codes.Codes()[0])
			if kind == "session" {
				backend, err := sessionpg.New(s.db, sessionpg.Config{Schema: s.schema, Clock: s.clock})
				if err != nil {
					t.Fatal(err)
				}
				store, err := session.NewStore(backend, session.DefaultConfig(s.namespace))
				if err != nil {
					t.Fatal(err)
				}
				other, err := session.New(store, "other.web", s.provider, "other.session")
				if err != nil {
					t.Fatal(err)
				}
				if result, err := other.CompleteMFA(t.Context(), pending, check, session.IssueOptions{}); !errors.Is(err, auth.Unauthenticated) || !result.Secret().IsZero() {
					t.Fatal("pending session crossed guards", err)
				}
			} else {
				backend, err := tokenpg.New(s.db, tokenpg.Config{Schema: s.schema, Clock: s.clock})
				if err != nil {
					t.Fatal(err)
				}
				store, err := token.NewStore(backend, token.DefaultConfig(s.namespace))
				if err != nil {
					t.Fatal(err)
				}
				other, err := token.New(store, "other.api", s.provider, "other.bearer", auth.AccessScopes[multifactor.Account]{})
				if err != nil {
					t.Fatal(err)
				}
				if result, err := other.CompleteMFA(t.Context(), pending, check, token.IssueOptions[multifactor.Account]{}); !errors.Is(err, auth.Unauthenticated) || !result.AccessSecret().IsZero() {
					t.Fatal("pending token crossed guards", err)
				}
				extra, err := auth.NewAccessScopes(auth.DefineAccessScope[multifactor.Account]("undeclared.admin"))
				if err != nil {
					t.Fatal(err)
				}
				if result, err := s.tokens.CompleteMFA(t.Context(), pending, check, token.IssueOptions[multifactor.Account]{Scopes: extra}); !errors.Is(err, auth.Forbidden) || !result.AccessSecret().IsZero() {
					t.Fatal("MFA widened token scope ceiling", err)
				}
			}
			if _, err := kernel.complete(t.Context(), pending, check); err != nil {
				t.Fatal("boundary rejection consumed credential/factor", err)
			}
		})
	}
}

func TestPostgresMFACompletionAfterCommitFailureCannotReuseChallenge(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, kind)
			pending := pendingCredential(t, kernel)
			cause := errors.New("after commit failed")
			wrapper := &completionBackend{Backend: s.backend, after: func(_ context.Context, tx *database.Tx) error {
				return tx.AfterCommit(func(context.Context) error { return cause })
			}}
			s.completionBackend(t, wrapper)
			raw, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[0]))
			var failure *database.Error
			if err == nil || !raw.IsZero() || !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Outcome() != database.Committed {
				t.Fatal("lost committed completion outcome", err)
			}
			wrapper.after = nil
			if raw, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[1])); !errors.Is(err, auth.Unauthenticated) || !raw.IsZero() {
				t.Fatal("committed challenge reused", err)
			}
		})
	}
}

func TestPostgresMFACompletionConsumesTOTPStepAcrossPendingCredentials(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			enrollment, codes := s.enroll(t)
			s.clock.Advance(30 * time.Second)
			kernel := s.kernel(t, kind)
			first, second := pendingCredential(t, kernel), pendingCredential(t, kernel)
			response, err := mfa.TOTPResponse(authenticatorCode(t, enrollment.Secret(), s.clock.Now()))
			if err != nil {
				t.Fatal(err)
			}
			check, err := s.factors.Verifier(response)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := kernel.complete(t.Context(), first, check); err != nil {
				t.Fatal(err)
			}
			if raw, err := kernel.complete(t.Context(), second, check); !errors.Is(err, auth.Unauthenticated) || !raw.IsZero() {
				t.Fatal("TOTP step authorized another challenge", err)
			}
			if err := kernel.require(t, second); !errors.Is(err, auth.MFARequired) {
				t.Fatal("factor failure consumed the second challenge", err)
			}
			if _, err := kernel.complete(t.Context(), second, verifier(t, s, codes.Codes()[0])); err != nil {
				t.Fatal("recovery fallback after TOTP rejection", err)
			}
		})
	}
}

func TestPostgresMFACompletionRejectsBrokenSecondFactorAdapters(t *testing.T) {
	for _, mode := range []string{"omit", "repeat", "foreign-scope", "panic"} {
		t.Run(mode, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, "session")
			pending := pendingCredential(t, kernel)
			broken := auth.DefineSecondFactor(s.provider, func(ctx context.Context, tx *database.Tx, _ model.Reference[multifactor.Account, model.ID[multifactor.Account]], consume func(context.Context, *database.Tx) error) error {
				switch mode {
				case "omit":
					return nil
				case "repeat":
					_ = consume(ctx, tx)
					_ = consume(ctx, tx)
					return nil
				case "foreign-scope":
					_ = consume(ctx, &database.Tx{})
					return nil
				case "panic":
					if err := consume(ctx, tx); err != nil {
						return err
					}
					panic("private adapter error")
				}
				return nil
			})
			if raw, err := kernel.complete(t.Context(), pending, broken); err == nil || !raw.IsZero() {
				t.Fatal("broken adapter published credentials")
			}
			if _, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[0])); err != nil {
				t.Fatal("broken adapter consumed challenge", err)
			}
		})
	}
}

func TestPostgresMFACompletionRejectsFactorFromAnotherPool(t *testing.T) {
	for _, kind := range []string{"session", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := mfaSetup(t)
			_, codes := s.enroll(t)
			kernel := s.kernel(t, kind)
			pending := pendingCredential(t, kernel)
			other, err := mfapg.New(pgtest.Open(t), mfapg.Config{Schema: s.schema, Clock: s.clock})
			if err != nil {
				t.Fatal(err)
			}
			s.completionBackend(t, other)
			if raw, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[0])); err == nil || !raw.IsZero() {
				t.Fatal("different pool joined credential transaction")
			}
			s.completionBackend(t, s.backend)
			if _, err := kernel.complete(t.Context(), pending, verifier(t, s, codes.Codes()[0])); err != nil {
				t.Fatal("pool rejection consumed pending or factor", err)
			}
		})
	}
}
