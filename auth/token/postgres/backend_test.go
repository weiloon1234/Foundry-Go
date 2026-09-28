package postgres_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/tokenstore"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type member struct {
	ID      int64
	Enabled bool
}

func (m member) reference() model.Reference[member, int64] {
	return model.NewReference[member]("token_members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

type setup struct {
	db       *database.DB
	schema   string
	clock    *testkit.Clock
	backend  *tokenpg.Backend
	tokens   *token.Tokens[member, int64]
	store    *token.Store
	provider auth.Provider[member, int64]
	config   token.Config
	grants   auth.AccessScopes[member]
	registry *auth.Registry
	enabled  atomic.Bool
	loads    atomic.Int32
}

func prepare(t *testing.T) *setup {
	t.Helper()
	s := &setup{db: pgtest.Open(t), clock: testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))}
	s.schema = pgtest.Namespace(t, s.db)
	s.enabled.Store(true)
	config := tokenpg.DefaultConfig()
	config.Schema = s.schema
	config.Clock = s.clock
	var err error
	s.backend, err = tokenpg.New(s.db, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.New(tokenpg.Migrations()...); err != nil {
		t.Fatal(err)
	}
	within(t, s, func(tx *database.Tx) error {
		for _, definition := range tokenpg.Migrations() {
			for _, sql := range definition.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		return nil
	})
	s.config = token.DefaultConfig(keyspace.Namespace{Application: "foundry-token-tests", Environment: "test"})
	s.config.Personal = token.Lifetime{Access: 20 * time.Minute, Absolute: 20 * time.Minute}
	s.config.Renewable = token.Lifetime{Access: 2 * time.Minute, RefreshIdle: 10 * time.Minute, Absolute: 30 * time.Minute}
	s.config.MaxPerSubject = 2
	s.config.MaxRotations = 3
	s.store, err = token.NewStore(s.backend, s.config)
	if err != nil {
		t.Fatal(err)
	}
	s.provider = auth.DefineProvider("members", member{}.reference(), func(_ context.Context, id int64) (value.Optional[member], error) {
		s.loads.Add(1)
		return value.Set(member{ID: id, Enabled: s.enabled.Load()}), nil
	}, func(_ context.Context, m member) (bool, error) { return m.Enabled, nil })
	s.grants, err = auth.NewAccessScopes(auth.DefineAccessScope[member]("orders.read"))
	if err != nil {
		t.Fatal(err)
	}
	s.tokens, err = token.New(s.store, "members.api", s.provider, "api.bearer", s.grants)
	if err != nil {
		t.Fatal(err)
	}
	s.registry, err = auth.NewRegistry(auth.DefaultConfig(), s.tokens.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func within(t *testing.T, s *setup, fn func(*database.Tx) error) {
	t.Helper()
	if err := s.db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+s.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}); err != nil {
		t.Fatal(err)
	}
}
func issue(t *testing.T, s *setup, id int64, refresh bool) token.Issued[member, int64] {
	t.Helper()
	proof, err := auth.NewProof(member{ID: id}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.tokens.Issue(t.Context(), proof, token.IssueOptions[member]{Name: "Test device", Scopes: s.grants, Refresh: refresh})
	if err != nil {
		t.Fatal(err)
	}
	return issued
}
func refreshSecret(t *testing.T, issued token.Issued[member, int64]) secret.String {
	t.Helper()
	raw, ok := issued.RefreshSecret().Get()
	if !ok {
		t.Fatal("missing refresh secret")
	}
	return raw
}
func authenticate(t *testing.T, s *setup, raw secret.String) error {
	t.Helper()
	credentials, err := auth.NewCredentials(auth.Credential{Name: "api.bearer", Secret: raw})
	if err != nil {
		return err
	}
	scope, err := s.registry.NewScope(t.Context(), credentials)
	if err != nil {
		return err
	}
	defer scope.Close()
	_, err = s.tokens.Guard().RequireScopes(scope.Context(), s.grants)
	return err
}

func TestPostgresTokenRotationRetainsHistoryAndReuseRevokesFamily(t *testing.T) {
	s := prepare(t)
	first := issue(t, s, 7, true)
	independent := issue(t, s, 7, false)
	if err := authenticate(t, s, first.AccessSecret()); err != nil {
		t.Fatal(err)
	}
	rotated, err := s.tokens.Refresh(t.Context(), refreshSecret(t, first))
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Info().ID() != first.Info().ID() || rotated.Info().ExpiresAt() != first.Info().ExpiresAt() || rotated.Info().Generation() != 1 {
		t.Fatal("rotation reset family")
	}
	if err := authenticate(t, s, first.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("old access survived", err)
	}
	if err := authenticate(t, s, rotated.AccessSecret()); err != nil {
		t.Fatal(err)
	}
	within(t, s, func(tx *database.Tx) error {
		rows, err := tokenstore.QueryFoundryTokenGenerations().All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 3 {
			t.Error("consumed generation not retained", len(rows))
		}
		for _, row := range rows {
			if row.AccessHash == first.AccessSecret().Reveal() {
				t.Error("stored raw access secret")
			}
			if hash, present := row.RefreshHash.Get(); present && hash == refreshSecret(t, first).Reveal() {
				t.Error("stored raw refresh secret")
			}
		}
		return nil
	})
	if rejected, err := s.tokens.Refresh(t.Context(), refreshSecret(t, first)); !errors.Is(err, auth.Unauthenticated) || !rejected.AccessSecret().IsZero() {
		t.Fatal("consumed refresh reused", err)
	}
	if err := authenticate(t, s, rotated.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("replay revocation rolled back", err)
	}
	if err := authenticate(t, s, independent.AccessSecret()); err != nil {
		t.Fatal("unrelated family revoked", err)
	}
	within(t, s, func(tx *database.Tx) error {
		n, err := tokenstore.QueryFoundryTokenGenerations().Count(t.Context(), tx)
		if n != 1 {
			t.Error("family history orphaned", n)
		}
		return err
	})
}

func TestConcurrentRefreshHasOneSuccessAndReplayRevokesSuccessor(t *testing.T) {
	s := prepare(t)
	first := issue(t, s, 7, true)
	raw := refreshSecret(t, first)
	start := make(chan struct{})
	type outcome struct {
		issued token.Issued[member, int64]
		err    error
	}
	results := make(chan outcome, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() { <-start; issued, err := s.tokens.Refresh(t.Context(), raw); results <- outcome{issued, err} })
	}
	close(start)
	workers.Wait()
	close(results)
	successes, rejections := 0, 0
	var successor token.Issued[member, int64]
	for result := range results {
		if result.err == nil {
			successes++
			successor = result.issued
		} else if errors.Is(result.err, auth.Unauthenticated) {
			rejections++
		} else {
			t.Fatal(result.err)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatal(successes, rejections)
	}
	if err := authenticate(t, s, successor.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("concurrent reuse left successor live", err)
	}
}

func TestTokenExpiryTouchAndRotationLimit(t *testing.T) {
	s := prepare(t)
	current := issue(t, s, 7, true)
	s.clock.Advance(time.Minute)
	if touched, err := s.tokens.Touch(t.Context(), current.AccessSecret()); err != nil || !touched {
		t.Fatal(touched, err)
	}
	info, err := s.tokens.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(info) != 1 {
		t.Fatal(err, len(info))
	}
	if info[0].AccessExpiresAt() != current.Info().AccessExpiresAt() || info[0].RefreshExpiresAt() != current.Info().RefreshExpiresAt() {
		t.Fatal("touch extended lifetime")
	}
	s.clock.Advance(2 * time.Minute)
	if err := authenticate(t, s, current.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("expired access authenticated", err)
	}
	for range s.config.MaxRotations {
		next, err := s.tokens.Refresh(t.Context(), refreshSecret(t, current))
		if err != nil {
			t.Fatal(err)
		}
		if next.Info().ExpiresAt() != current.Info().ExpiresAt() {
			t.Fatal("absolute expiry restarted")
		}
		current = next
	}
	if _, err := s.tokens.Refresh(t.Context(), refreshSecret(t, current)); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("rotation capacity exceeded", err)
	}
	if err := authenticate(t, s, current.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("exhausted family left live", err)
	}
}

func TestConcurrentIssueCapacityAndSubjectRevocation(t *testing.T) {
	s := prepare(t)
	verified, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			<-start
			_, err := s.tokens.Issue(t.Context(), verified, token.IssueOptions[member]{Scopes: s.grants, Refresh: true})
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
	}
	if successes != 2 {
		t.Fatal("subject capacity raced", successes)
	}
	list, err := s.tokens.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(list) != 2 {
		t.Fatal(err, len(list))
	}
	if removed, err := s.tokens.RevokeID(t.Context(), member{ID: 8}.reference(), list[0].ID()); err != nil || removed {
		t.Fatal("foreign subject revoked", err)
	}
	if count, err := s.tokens.RevokeAll(t.Context(), member{ID: 7}.reference()); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if list, err := s.tokens.List(t.Context(), member{ID: 7}.reference()); err != nil || len(list) != 0 {
		t.Fatal(err, len(list))
	}
	_ = issue(t, s, 7, true)
}

func TestPruneCurrentIdleExpiryAndRefreshProviderState(t *testing.T) {
	s := prepare(t)
	first := issue(t, s, 7, true)
	s.enabled.Store(false)
	if err := authenticate(t, s, first.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("disabled subject accepted", err)
	}
	s.enabled.Store(true)
	if err := authenticate(t, s, first.AccessSecret()); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(9 * time.Minute)
	next, err := s.tokens.Refresh(t.Context(), refreshSecret(t, first))
	if err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(2 * time.Minute)
	if n, err := s.tokens.Prune(t.Context(), token.MaxPruneFamilies); err != nil || n != 0 {
		t.Fatal("historical expiry pruned live successor", n, err)
	}
	s.clock.Advance(9 * time.Minute)
	if n, err := s.tokens.Prune(t.Context(), token.MaxPruneFamilies); err != nil || n != 1 {
		t.Fatal("idle-expired family not pruned", n, err)
	}
	if err := authenticate(t, s, next.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	within(t, s, func(tx *database.Tx) error {
		n, err := tokenstore.QueryFoundryTokenGenerations().Count(t.Context(), tx)
		if n != 0 {
			t.Error("prune left generations", n)
		}
		return err
	})
}

func TestTokenRefreshChecksExpiryAfterSubjectLock(t *testing.T) {
	s := prepare(t)
	first := issue(t, s, 7, true)
	var family tokenstore.Family
	within(t, s, func(tx *database.Tx) error {
		rows, err := tokenstore.QueryFoundryTokenFamilies().All(t.Context(), tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			t.Fatal("missing family")
		}
		family = rows[0]
		return nil
	})
	locked, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	held := make(chan error, 1)
	go func() {
		held <- s.db.Transaction(t.Context(), func(tx *database.Tx) error {
			if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+s.schema+`"`); err != nil {
				return err
			}
			if _, err := tokenstore.QueryFoundryTokenSubjects().ForUpdate().Find(t.Context(), tx, family.SubjectKey); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-held:
		t.Fatal("subject lock", err)
	}
	type outcome struct {
		issued token.Issued[member, int64]
		err    error
	}
	done := make(chan outcome, 1)
	raw := refreshSecret(t, first)
	go func() { issued, err := s.tokens.Refresh(t.Context(), raw); done <- outcome{issued, err} }()
	select {
	case result := <-done:
		t.Fatal("refresh escaped lock", result.err)
	case <-time.After(20 * time.Millisecond):
	}
	s.clock.Advance(11 * time.Minute)
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	result := <-done
	if !errors.Is(result.err, auth.Unauthenticated) || !result.issued.AccessSecret().IsZero() {
		t.Fatal("refresh revived expired credential", result.err)
	}
	if listed, err := s.tokens.List(t.Context(), member{ID: 7}.reference()); err != nil || len(listed) != 0 {
		t.Fatal("expired family survived", err)
	}
}
func TestTokenAddressesIsolateGuardProviderAndEnvironment(t *testing.T) {
	s := prepare(t)
	first := issue(t, s, 7, true)
	for _, kind := range []string{"guard", "provider", "environment"} {
		t.Run(kind, func(t *testing.T) {
			provider := s.provider
			name := s.tokens.Guard().Name()
			config := s.config
			switch kind {
			case "guard":
				name = "other.api"
			case "provider":
				provider = auth.DefineProvider("other-members", member{}.reference(), func(_ context.Context, id int64) (value.Optional[member], error) {
					return value.Set(member{ID: id, Enabled: true}), nil
				}, func(context.Context, member) (bool, error) { return true, nil })
			case "environment":
				config.Namespace.Environment = "other"
			}
			store, err := token.NewStore(s.backend, config)
			if err != nil {
				t.Fatal(err)
			}
			other, err := token.New(store, name, provider, "other.bearer", s.grants)
			if err != nil {
				t.Fatal(err)
			}
			if list, err := other.List(t.Context(), member{ID: 7}.reference()); err != nil || len(list) != 0 {
				t.Fatal("cross-address listing", err)
			}
			if ok, err := other.RevokeID(t.Context(), member{ID: 7}.reference(), first.Info().ID()); err != nil || ok {
				t.Fatal("cross-address revocation", err)
			}
			if _, err := other.Refresh(t.Context(), refreshSecret(t, first)); !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("cross-address refresh", err)
			}
			if err := authenticate(t, s, first.AccessSecret()); err != nil {
				t.Fatal("foreign operation changed original", err)
			}
		})
	}
}
func TestTokenMissingMigrationsDoesNotPublishSecret(t *testing.T) {
	s := prepare(t)
	config := tokenpg.DefaultConfig()
	config.Schema = pgtest.Namespace(t, s.db)
	config.Clock = s.clock
	backend, err := tokenpg.New(s.db, config)
	if err != nil {
		t.Fatal(err)
	}
	store, err := token.NewStore(backend, s.config)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := token.New(store, "missing", s.provider, "missing.bearer", s.grants)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := tokens.Issue(t.Context(), proof, token.IssueOptions[member]{Refresh: true})
	if err == nil || !issued.AccessSecret().IsZero() || issued.RefreshSecret().IsSet() {
		t.Fatal("missing storage published credential", err)
	}
}
