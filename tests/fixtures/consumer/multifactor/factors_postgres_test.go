package multifactor_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	lockoutmemory "github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	mfapg "github.com/weiloon1234/Foundry-Go/auth/mfa/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type lateLockout struct {
	lockout.Backend
	reject       atomic.Bool
	afterSuccess func()
}

func (b *lateLockout) LockoutFinish(ctx context.Context, key lockout.Key, policy lockout.Policy, snapshot lockout.Snapshot, outcome lockout.Outcome) (lockout.Decision, error) {
	if outcome == lockout.Succeeded && b.reject.Load() {
		return lockout.Decision{Status: lockout.StatusLocked, RetryAfter: time.Minute}, nil
	}
	decision, err := b.Backend.LockoutFinish(ctx, key, policy, snapshot, outcome)
	if err == nil && outcome == lockout.Succeeded && b.afterSuccess != nil {
		b.afterSuccess()
	}
	return decision, err
}

type mfaFixture struct {
	db              *database.DB
	schema          string
	clock           *testkit.Clock
	namespace       keyspace.Namespace
	account         multifactor.Account
	provider        multifactor.Provider
	hasher          *password.Hasher
	factors         *multifactor.Factors
	store           *mfa.Store
	backend         *mfapg.Backend
	attempts        lockout.Throttle[model.ID[multifactor.Account]]
	locks           *lateLockout
	key             encryption.Key
	sessions        *session.Sessions[multifactor.Account, model.ID[multifactor.Account]]
	tokens          *token.Tokens[multifactor.Account, model.ID[multifactor.Account]]
	revocations     *auth.Revocations[multifactor.Account, model.ID[multifactor.Account]]
	afterInvalidate func(context.Context, *database.Tx) error
	providerLookups atomic.Int32
}

func (s *mfaFixture) within(ctx context.Context, fn func(*database.Tx) error) error {
	return s.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	})
}
func mfaSetup(t *testing.T) *mfaFixture {
	t.Helper()
	s := &mfaFixture{db: pgtest.Open(t), clock: testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)), namespace: keyspace.Namespace{Application: "foundry-mfa-tests", Environment: "test"}}
	s.schema = pgtest.Namespace(t, s.db)
	config := password.DefaultConfig()
	config.Parameters = password.Parameters{MemoryKiB: 19 * 1024, Iterations: 2, Parallelism: 1}
	var err error
	s.hasher, err = password.New(config)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := password.NewPlaintext(secret.New("private MFA password"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	definitions := append(mfapg.Migrations(), sessionpg.Migrations()...)
	definitions = append(definitions, tokenpg.Migrations()...)
	if _, err := migrate.New(definitions...); err != nil {
		t.Fatal(err)
	}
	err = s.within(t.Context(), func(tx *database.Tx) error {
		for _, definition := range definitions {
			for _, sql := range definition.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(t.Context(), `CREATE TABLE mfa_accounts(id uuid PRIMARY KEY,email text NOT NULL,password text NOT NULL,enabled boolean NOT NULL,mfa_enabled boolean NOT NULL,require_mfa boolean NOT NULL)`); err != nil {
			return err
		}
		s.account, err = multifactor.QueryMfaAccounts().Create(t.Context(), tx, multifactor.AccountDraft{}.SetEmail("member@example.test").SetPassword(hash).SetEnabled(true).SetMFAEnabled(false).SetRequireMFA(false))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s.provider = auth.DefineProvider("mfa.accounts", (multifactor.Account{}).FoundryReference(), func(ctx context.Context, id model.ID[multifactor.Account]) (value.Optional[multifactor.Account], error) {
		s.providerLookups.Add(1)
		var found value.Optional[multifactor.Account]
		err := s.within(ctx, func(tx *database.Tx) error {
			var err error
			found, err = multifactor.QueryMfaAccounts().Find(ctx, tx, id)
			return err
		})
		return found, err
	}, func(_ context.Context, account multifactor.Account) (bool, error) { return account.Enabled, nil })
	s.backend, err = mfapg.New(s.db, mfapg.Config{Schema: s.schema, Clock: s.clock})
	if err != nil {
		t.Fatal(err)
	}
	s.key, err = encryption.GenerateKey("first")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := encryption.NewKeyring(s.key.ID(), s.key)
	if err != nil {
		t.Fatal(err)
	}
	s.store, err = mfa.NewStore(s.backend, keys, mfa.DefaultConfig(s.namespace, "Foundry test"))
	if err != nil {
		t.Fatal(err)
	}
	memory, err := lockoutmemory.New(128, s.clock)
	if err != nil {
		t.Fatal(err)
	}
	s.locks = &lateLockout{Backend: memory}
	attempts, err := lockout.NewStore(s.locks, lockout.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	s.attempts, err = multifactor.FactorAttempts.Bind(attempts)
	if err != nil {
		t.Fatal(err)
	}
	webBackend, err := sessionpg.New(s.db, sessionpg.Config{Schema: s.schema, Clock: s.clock})
	if err != nil {
		t.Fatal(err)
	}
	webStore, err := session.NewStore(webBackend, session.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	s.sessions, err = session.New(webStore, "mfa.web", s.provider, "mfa.session")
	if err != nil {
		t.Fatal(err)
	}
	apiBackend, err := tokenpg.New(s.db, tokenpg.Config{Schema: s.schema, Clock: s.clock})
	if err != nil {
		t.Fatal(err)
	}
	apiStore, err := token.NewStore(apiBackend, token.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	s.tokens, err = token.New(apiStore, "mfa.api", s.provider, "mfa.bearer", auth.AccessScopes[multifactor.Account]{})
	if err != nil {
		t.Fatal(err)
	}
	web, err := s.sessions.Revocation()
	if err != nil {
		t.Fatal(err)
	}
	api, err := s.tokens.Revocation()
	if err != nil {
		t.Fatal(err)
	}
	s.revocations, err = auth.NewRevocations(s.provider, web, api)
	if err != nil {
		t.Fatal(err)
	}
	s.factors, err = multifactor.NewFactors(s.store, s.provider, s.attempts, s.invalidate)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (s *mfaFixture) invalidate(ctx context.Context, tx *database.Tx, account multifactor.Account) error {
	if err := s.revocations.Invalidate(ctx, tx, account); err != nil {
		return err
	}
	if s.afterInvalidate != nil {
		return s.afterInvalidate(ctx, tx)
	}
	return nil
}
func (s *mfaFixture) login(t *testing.T) multifactor.PasswordResult {
	t.Helper()
	var result multifactor.PasswordResult
	err := s.within(t.Context(), func(tx *database.Tx) error {
		login, err := multifactor.NewLogin(tx, s.provider, s.hasher)
		if err != nil {
			return err
		}
		plain, err := password.NewPlaintext(secret.New("private MFA password"))
		if err != nil {
			return err
		}
		result, err = login.Authenticate(t.Context(), s.account.Email, plain)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func (s *mfaFixture) current(t *testing.T) multifactor.Account {
	t.Helper()
	var account multifactor.Account
	err := s.within(t.Context(), func(tx *database.Tx) error {
		found, err := multifactor.QueryMfaAccounts().Find(t.Context(), tx, s.account.ID)
		if err != nil {
			return err
		}
		var exists bool
		account, exists = found.Get()
		if !exists {
			return errors.New("fixture account missing")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}
func (s *mfaFixture) issue(t *testing.T, result multifactor.PasswordResult) {
	t.Helper()
	if _, err := s.sessions.Issue(t.Context(), result.Proof(), session.IssueOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.tokens.Issue(t.Context(), result.Proof(), token.IssueOptions[multifactor.Account]{Name: "MFA fixture"}); err != nil {
		t.Fatal(err)
	}
}
func (s *mfaFixture) countCredentials(t *testing.T, want int) {
	t.Helper()
	sessions, err := s.sessions.List(t.Context(), s.account.FoundryReference())
	if err != nil || len(sessions) != want {
		t.Fatalf("session count=%d want=%d: %v", len(sessions), want, err)
	}
	tokens, err := s.tokens.List(t.Context(), s.account.FoundryReference())
	if err != nil || len(tokens) != want {
		t.Fatalf("token count=%d want=%d: %v", len(tokens), want, err)
	}
}

// authenticatorCode is an independent client-side HOTP construction; framework
// matching stays private so tests cannot accidentally use the verifier as oracle.
func authenticatorCode(t *testing.T, key mfa.TOTPSecret, now time.Time) mfa.TOTPCode {
	t.Helper()
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(key.Secret().Reveal())
	if err != nil {
		t.Fatal(err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, raw)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	digits := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	code, err := mfa.ParseTOTPCode(secret.New(fmt.Sprintf("%06d", digits%1000000)))
	if err != nil {
		t.Fatal(err)
	}
	return code
}
func recoveryResponse(t *testing.T, code mfa.RecoveryCode) mfa.Response {
	t.Helper()
	r, err := mfa.RecoveryResponse(code)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (s *mfaFixture) enroll(t *testing.T) (mfa.Enrollment[multifactor.Account], mfa.RecoveryCodes[multifactor.Account]) {
	t.Helper()
	password := s.login(t)
	enrollment, err := s.factors.Enroll(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.factors.Confirm(t.Context(), password, enrollment.ID(), authenticatorCode(t, enrollment.Secret(), s.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return enrollment, codes
}

func TestPostgresMFAEnrollmentReplacementConfirmationAndRevocation(t *testing.T) {
	s := mfaSetup(t)
	password := s.login(t)
	s.issue(t, password)
	first, err := s.factors.Enroll(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.factors.Enroll(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() == latest.ID() || first.Secret() == latest.Secret() {
		t.Fatal("replacement retained enrollment generation")
	}
	if _, err := s.factors.Confirm(t.Context(), password, first.ID(), authenticatorCode(t, first.Secret(), s.clock.Now())); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("replaced enrollment accepted", err)
	}
	codes, err := s.factors.Confirm(t.Context(), password, latest.ID(), authenticatorCode(t, latest.Secret(), s.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes.Codes()) != mfa.DefaultRecoveryCodes || !codes.Subject().MFAEnabled || !s.current(t).MFAEnabled {
		t.Fatal("confirmation state")
	}
	s.countCredentials(t, 0)
	if _, err := s.sessions.Issue(t.Context(), password.Proof(), session.IssueOptions{}); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("pre-enrollment password issued credentials", err)
	}
	current := s.login(t)
	if current.Proof().Assurance() != auth.PendingMFA {
		t.Fatal("enabled factor did not require MFA")
	}
	if _, err := s.factors.Enroll(t.Context(), current); err == nil {
		t.Fatal("confirmed factor replaced without verification")
	}
	used, _ := mfa.TOTPResponse(authenticatorCode(t, latest.Secret(), s.clock.Now()))
	if _, err := s.factors.RegenerateRecovery(t.Context(), current, used); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("confirmation TOTP replayed", err)
	}
	s.issue(t, current)
	rotated, err := s.factors.RegenerateRecovery(t.Context(), current, recoveryResponse(t, codes.Codes()[0]))
	if err != nil {
		t.Fatal(err)
	}
	s.countCredentials(t, 0)
	if _, err := s.factors.Disable(t.Context(), current, recoveryResponse(t, codes.Codes()[1])); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("old recovery set survived rotation", err)
	}
	disabled, err := s.factors.Disable(t.Context(), current, recoveryResponse(t, rotated.Codes()[0]))
	if err != nil || disabled.MFAEnabled || s.current(t).MFAEnabled {
		t.Fatal("disable", err)
	}
	if s.login(t).Proof().Assurance() != auth.Authenticated {
		t.Fatal("disabled optional factor still required")
	}
	if _, err := s.factors.Enroll(t.Context(), s.login(t)); err != nil {
		t.Fatal("reenroll after verified disable", err)
	}
}

func TestPostgresMFAConfirmationRollsBackEveryDatabaseChange(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "cancel", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			s := mfaSetup(t)
			password := s.login(t)
			s.issue(t, password)
			enrollment, err := s.factors.Enroll(t.Context(), password)
			if err != nil {
				t.Fatal(err)
			}
			code := authenticatorCode(t, enrollment.Secret(), s.clock.Now())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("late domain mutation failed")
			s.afterInvalidate = func(context.Context, *database.Tx) error {
				switch mode {
				case "error":
					return cause
				case "panic":
					panic("private domain failure")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					cancel()
				case "expiry":
					s.clock.Advance(10 * time.Minute)
				}
				return nil
			}
			result, err := s.factors.Confirm(ctx, password, enrollment.ID(), code)
			if err == nil || len(result.Codes()) != 0 || !result.ID().IsZero() {
				t.Fatal("failed confirmation exposed result")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if s.current(t).MFAEnabled {
				t.Fatal("flag survived failed transaction")
			}
			s.countCredentials(t, 1)
			s.afterInvalidate = nil
			if mode != "expiry" {
				if _, err := s.factors.Confirm(t.Context(), password, enrollment.ID(), code); err != nil {
					t.Fatal("failed transaction consumed enrollment/TOTP", err)
				}
			} else if _, err := s.factors.Confirm(t.Context(), password, enrollment.ID(), code); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("expired enrollment accepted", err)
			}
		})
	}
}

func TestPostgresMFALateLockoutDoesNotConsumeRecoveryOrWrite(t *testing.T) {
	s := mfaSetup(t)
	_, codes := s.enroll(t)
	password := s.login(t)
	s.issue(t, password)
	response := recoveryResponse(t, codes.Codes()[0])
	s.locks.reject.Store(true)
	if result, err := s.factors.RegenerateRecovery(t.Context(), password, response); err == nil || len(result.Codes()) != 0 {
		t.Fatal("late lockout allowed protected action")
	}
	s.countCredentials(t, 1)
	s.locks.reject.Store(false)
	if _, err := s.factors.RegenerateRecovery(t.Context(), password, response); err != nil {
		t.Fatal("late rejection consumed recovery code", err)
	}
	s.countCredentials(t, 0)
}

func TestPostgresMFAConcurrentRecoveryIsConsumedOnce(t *testing.T) {
	s := mfaSetup(t)
	_, codes := s.enroll(t)
	password := s.login(t)
	response := recoveryResponse(t, codes.Codes()[0])
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := s.factors.RegenerateRecovery(t.Context(), password, response)
			results <- err
		}()
	}
	close(start)
	succeeded, rejected := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, auth.Unauthenticated) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatal("recovery consumption was not atomic")
	}
}

func TestPostgresMFAPrunesOnlyPendingAndPreservesRequiredPolicy(t *testing.T) {
	s := mfaSetup(t)
	password := s.login(t)
	enrollment, err := s.factors.Enroll(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(10 * time.Minute)
	if _, err := s.factors.Confirm(t.Context(), password, enrollment.ID(), authenticatorCode(t, enrollment.Secret(), s.clock.Now())); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("expired enrollment confirmed", err)
	}
	if removed, err := s.factors.Prune(t.Context(), 1); err != nil || removed != 1 {
		t.Fatal("pending prune", removed, err)
	}
	_, codes := s.enroll(t)
	s.clock.Advance(time.Hour)
	if removed, err := s.factors.Prune(t.Context(), mfa.MaxPrune); err != nil || removed != 0 {
		t.Fatal("confirmed factor pruned", removed, err)
	}
	err = s.within(t.Context(), func(tx *database.Tx) error {
		_, err := multifactor.QueryMfaAccounts().Update(t.Context(), tx, s.account.ID, multifactor.AccountDraft{}.SetRequireMFA(true))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.factors.Disable(t.Context(), s.login(t), recoveryResponse(t, codes.Codes()[0])); !errors.Is(err, auth.Forbidden) {
		t.Fatal("required factor disabled", err)
	}
	if !s.current(t).MFAEnabled {
		t.Fatal("required policy failure changed model")
	}
}

func TestPostgresMFAKeyRotationPreservesEnrollmentAndRecoveryState(t *testing.T) {
	s := mfaSetup(t)
	enrollment, codes := s.enroll(t)
	password := s.login(t)
	next, err := encryption.GenerateKey("next")
	if err != nil {
		t.Fatal(err)
	}
	during, err := encryption.NewKeyring(next.ID(), s.key, next)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mfa.NewStore(s.backend, during, mfa.DefaultConfig(s.namespace, "Foundry test"))
	if err != nil {
		t.Fatal(err)
	}
	factors, err := multifactor.NewFactors(store, s.provider, s.attempts, s.invalidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := factors.Reencrypt(t.Context(), password, recoveryResponse(t, codes.Codes()[0])); err != nil {
		t.Fatal(err)
	}
	onlyNext, err := encryption.NewKeyring(next.ID(), next)
	if err != nil {
		t.Fatal(err)
	}
	store, err = mfa.NewStore(s.backend, onlyNext, mfa.DefaultConfig(s.namespace, "Foundry test"))
	if err != nil {
		t.Fatal(err)
	}
	factors, err = multifactor.NewFactors(store, s.provider, s.attempts, s.invalidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := factors.Reencrypt(t.Context(), password, recoveryResponse(t, codes.Codes()[0])); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("rotation did not consume recovery code", err)
	}
	s.clock.Advance(30 * time.Second)
	response, _ := mfa.TOTPResponse(authenticatorCode(t, enrollment.Secret(), s.clock.Now()))
	if _, err := factors.RegenerateRecovery(t.Context(), password, response); err != nil {
		t.Fatal("TOTP changed or old key still required", err)
	}
}

func TestPostgresMFAAfterCommitErrorRetainsCommittedOutcome(t *testing.T) {
	s := mfaSetup(t)
	password := s.login(t)
	s.issue(t, password)
	enrollment, err := s.factors.Enroll(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("after-commit delivery failed")
	s.afterInvalidate = func(_ context.Context, tx *database.Tx) error {
		return tx.AfterCommit(func(context.Context) error { cancel(); return cause })
	}
	result, err := s.factors.Confirm(ctx, password, enrollment.ID(), authenticatorCode(t, enrollment.Secret(), s.clock.Now()))
	var databaseError *database.Error
	if err == nil || len(result.Codes()) != 0 || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !errors.As(err, &databaseError) || databaseError.Outcome() != database.Committed {
		t.Fatal("lost committed MFA outcome", err)
	}
	if !s.current(t).MFAEnabled {
		t.Fatal("committed factor flag missing")
	}
	s.countCredentials(t, 0)
	s.afterInvalidate = nil
	s.clock.Advance(30 * time.Second)
	response, _ := mfa.TOTPResponse(authenticatorCode(t, enrollment.Secret(), s.clock.Now()))
	if _, err := s.factors.RegenerateRecovery(t.Context(), s.login(t), response); err != nil {
		t.Fatal("cannot recover codes after committed delivery failure", err)
	}
}
