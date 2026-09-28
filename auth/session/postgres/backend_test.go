package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	databasepg "github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sessionstore"
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
	return model.NewReference[member]("session_members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

type setup struct {
	backend  *sessionpg.Backend
	store    *session.Store
	sessions *session.Sessions[member, int64]
	provider auth.Provider[member, int64]
	config   session.Config
	clock    *testkit.Clock
	db       *database.DB
	schema   string
	loads    atomic.Int32
	enabled  atomic.Bool
}

func prepare(t *testing.T, adjust ...func(*databasepg.Config)) *setup {
	t.Helper()
	db := pgtest.Open(t, adjust...)
	schema := pgtest.Namespace(t, db)
	clock := testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	config := sessionpg.DefaultConfig()
	config.Schema = schema
	config.Clock = clock
	backend, err := sessionpg.New(db, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.New(sessionpg.Migrations()...); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`"`); err != nil {
			return err
		}
		for _, definition := range sessionpg.Migrations() {
			for _, statement := range definition.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &setup{backend: backend, clock: clock, db: db, schema: schema}
	s.enabled.Store(true)
	s.config = session.DefaultConfig(keyspace.Namespace{Application: "foundry-session-tests", Environment: "test"})
	s.config.Regular = session.Lifetime{Idle: 10 * time.Minute, Absolute: 20 * time.Minute, Sliding: true}
	s.config.Remembered = session.Lifetime{Idle: 30 * time.Minute, Absolute: 40 * time.Minute, Sliding: true}
	s.config.Pending = session.Lifetime{Idle: time.Minute, Absolute: time.Minute}
	s.config.MaxPerSubject = 2
	s.store, err = session.NewStore(backend, s.config)
	if err != nil {
		t.Fatal(err)
	}
	s.provider = auth.DefineProvider("members", (member{}).reference(), func(_ context.Context, id int64) (value.Optional[member], error) {
		s.loads.Add(1)
		if id == 404 {
			return value.Optional[member]{}, nil
		}
		return value.Set(member{ID: id, Enabled: s.enabled.Load()}), nil
	}, func(_ context.Context, m member) (bool, error) { return m.Enabled, nil })
	s.sessions, err = session.New(s.store, "members.web", s.provider, "web.session")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func issue(t *testing.T, sessions *session.Sessions[member, int64], id int64, assurance auth.Assurance, remember bool) session.Issued[member, int64] {
	t.Helper()
	proof, err := auth.NewProof(member{ID: id}.reference(), assurance)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := sessions.Issue(t.Context(), proof, session.IssueOptions{Remember: remember})
	if err != nil {
		t.Fatal(err)
	}
	return issued
}
func authenticate(t *testing.T, sessions *session.Sessions[member, int64], credential secret.String) (member, error) {
	t.Helper()
	guard := sessions.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: guard.Source(), Secret: credential})
	if err != nil {
		return member{}, err
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	return guard.Require(scope.Context())
}
func stored(t *testing.T, s *setup) []sessionstore.Entry {
	t.Helper()
	var rows []sessionstore.Entry
	if err := s.db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+s.schema+`"`); err != nil {
			return err
		}
		var err error
		rows, err = sessionstore.QueryFoundrySessions().All(t.Context(), tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestSessionIssueStoresOnlyHashAndResolvesCurrentModel(t *testing.T) {
	s := prepare(t)
	issued := issue(t, s.sessions, 7, auth.Authenticated, false)
	if issued.Secret().IsZero() || issued.Info().ID().IsZero() || issued.Info().Subject().Key() != 7 {
		t.Fatal("issued metadata lost")
	}
	hash, err := session.HashSecret(issued.Secret())
	if err != nil {
		t.Fatal(err)
	}
	rows := stored(t, s)
	if len(rows) != 1 || rows[0].SecretHash != hash.Hex() || strings.Contains(rows[0].SecretHash, issued.Secret().Reveal()) {
		t.Fatal("session was not hashed")
	}
	got, err := authenticate(t, s.sessions, issued.Secret())
	if err != nil || got.ID != 7 {
		t.Fatal(got, err)
	}
	s.enabled.Store(false)
	if got, err := authenticate(t, s.sessions, issued.Secret()); !errors.Is(err, auth.Unauthenticated) || got != (member{}) {
		t.Fatal("disabled member authenticated", got, err)
	}
	s.enabled.Store(true)
	if got, err := authenticate(t, s.sessions, issued.Secret()); err != nil || got.ID != 7 {
		t.Fatal(err)
	}
	if s.loads.Load() != 3 {
		t.Fatal("unexpected model hydration count", s.loads.Load())
	}
	for _, value := range []any{issued, issued.Info(), hash, rows[0]} {
		if strings.Contains(fmt.Sprintf("%+v", value), issued.Secret().Reveal()) {
			t.Fatal("routine formatting leaked credential")
		}
	}
}

func TestSessionSlidingExpiryAndRotationKeepAbsoluteDeadline(t *testing.T) {
	s := prepare(t)
	initial := issue(t, s.sessions, 7, auth.Authenticated, false)
	s.clock.Advance(9 * time.Minute)
	if _, err := authenticate(t, s.sessions, initial.Secret()); err != nil {
		t.Fatal(err)
	}
	list, err := s.sessions.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	if !list[0].IdleExpiresAt().UTC().Equal(initial.Info().CreatedAt().UTC().Add(19 * time.Minute)) {
		t.Fatal("idle deadline did not slide")
	}
	s.clock.Advance(9 * time.Minute)
	rotated, err := s.sessions.Rotate(t.Context(), initial.Secret())
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Info().ID() != initial.Info().ID() || rotated.Info().CreatedAt() != initial.Info().CreatedAt() || rotated.Info().ExpiresAt() != initial.Info().ExpiresAt() || rotated.Secret().Reveal() == initial.Secret().Reveal() {
		t.Fatal("rotation changed lifetime or reused secret")
	}
	if _, err := authenticate(t, s.sessions, initial.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("old secret survived rotation", err)
	}
	if _, err := authenticate(t, s.sessions, rotated.Secret()); err != nil {
		t.Fatal(err)
	}
	s.clock.Advance(2 * time.Minute)
	if _, err := authenticate(t, s.sessions, rotated.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("absolute expiry extended", err)
	}
	if _, err := s.sessions.Rotate(t.Context(), rotated.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("expired session rotated", err)
	}
}

func TestSessionConcurrentRotationHasOneWinner(t *testing.T) {
	s := prepare(t)
	initial := issue(t, s.sessions, 7, auth.Authenticated, false)
	start := make(chan struct{})
	var wg sync.WaitGroup
	successes := make(chan session.Issued[member, int64], 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			<-start
			next, err := s.sessions.Rotate(t.Context(), initial.Secret())
			if err == nil {
				successes <- next
			} else {
				failures <- err
			}
		})
	}
	close(start)
	wg.Wait()
	close(successes)
	close(failures)
	if len(successes) != 1 || len(failures) != 7 {
		t.Fatal("concurrent rotation count", len(successes), len(failures))
	}
	for err := range failures {
		if !errors.Is(err, auth.Unauthenticated) {
			t.Fatal(err)
		}
	}
	winner := <-successes
	if _, err := authenticate(t, s.sessions, winner.Secret()); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(t, s.sessions, initial.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if len(stored(t, s)) != 1 {
		t.Fatal("rotation duplicated rows")
	}
}

func TestSessionConcurrentCreationEnforcesSubjectCapacity(t *testing.T) {
	s := prepare(t)
	proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			<-start
			_, err := s.sessions.Issue(t.Context(), proof, session.IssueOptions{})
			if err == nil {
				successes.Add(1)
			} else {
				failures <- err
			}
		})
	}
	close(start)
	wg.Wait()
	close(failures)
	if successes.Load() != 2 {
		t.Fatal("session capacity was raced", successes.Load())
	}
	for err := range failures {
		if !errors.Is(err, fault.Conflict) {
			t.Fatal(err)
		}
	}
	if len(stored(t, s)) != 2 {
		t.Fatal("failed creation persisted credentials")
	}
	s.clock.Advance(21 * time.Minute)
	issue(t, s.sessions, 7, auth.Authenticated, false)
	if len(stored(t, s)) != 1 {
		t.Fatal("expired sessions did not free capacity")
	}
}

func TestSessionRevokeIsolationPendingMFAAndRememberedPolicy(t *testing.T) {
	s := prepare(t)
	other, err := session.New(s.store, "members.other", s.provider, "other.session")
	if err != nil {
		t.Fatal(err)
	}
	first := issue(t, s.sessions, 7, auth.Authenticated, false)
	second := issue(t, s.sessions, 7, auth.Authenticated, true)
	foreign := issue(t, other, 7, auth.Authenticated, false)
	if !second.Info().Remembered() || second.Info().ExpiresAt().UTC().Sub(second.Info().CreatedAt().UTC()) != 40*time.Minute {
		t.Fatal("remembered policy missing")
	}
	if removed, err := other.Revoke(t.Context(), first.Secret()); err != nil || removed {
		t.Fatal("cross-guard revoke", removed, err)
	}
	if n, err := s.sessions.RevokeAll(t.Context(), member{ID: 7}.reference()); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	for _, credential := range []secret.String{first.Secret(), second.Secret()} {
		if _, err := authenticate(t, s.sessions, credential); !errors.Is(err, auth.Unauthenticated) {
			t.Fatal(err)
		}
	}
	if _, err := authenticate(t, other, foreign.Secret()); err != nil {
		t.Fatal("foreign guard revoked", err)
	}
	pending := issue(t, s.sessions, 8, auth.PendingMFA, false)
	before := s.loads.Load()
	if _, err := authenticate(t, s.sessions, pending.Secret()); !errors.Is(err, auth.MFARequired) {
		t.Fatal(err)
	}
	if s.loads.Load() != before {
		t.Fatal("pending MFA exposed model")
	}
	proof, err := auth.NewProof(member{ID: 9}.reference(), auth.PendingMFA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.sessions.Issue(t.Context(), proof, session.IssueOptions{Remember: true}); !errors.Is(err, fault.Invalid) {
		t.Fatal("remembered MFA-pending credential accepted", err)
	}
	if n, err := s.sessions.RevokeAll(t.Context(), member{ID: 404}.reference()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestSessionPruneIsBoundedAndScoped(t *testing.T) {
	s := prepare(t)
	other, err := session.New(s.store, "members.other", s.provider, "other.session")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3} {
		issue(t, s.sessions, id, auth.Authenticated, false)
	}
	foreign := issue(t, other, 7, auth.Authenticated, true)
	s.clock.Advance(21 * time.Minute)
	if count, err := s.sessions.Prune(t.Context(), 2); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if count, err := s.sessions.Prune(t.Context(), 2); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if count, err := s.sessions.Prune(t.Context(), 2); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err := authenticate(t, other, foreign.Secret()); err != nil {
		t.Fatal("prune changed foreign guard", err)
	}
	if len(stored(t, s)) != 1 {
		t.Fatal("scoped pruning lost rows")
	}
}

type uncertain struct {
	session.Backend
	create, rotate, revoke bool
	cause                  error
	calls                  atomic.Int32
}

func (b *uncertain) Create(ctx context.Context, a session.Address, c session.Creation) (session.Record, error) {
	r, err := b.Backend.Create(ctx, a, c)
	if err == nil && b.create {
		b.calls.Add(1)
		return session.Record{}, b.cause
	}
	return r, err
}
func (b *uncertain) Rotate(ctx context.Context, a session.Address, old, next session.Digest) (value.Optional[session.Record], error) {
	r, err := b.Backend.Rotate(ctx, a, old, next)
	if err == nil && b.rotate {
		b.calls.Add(1)
		return value.Optional[session.Record]{}, b.cause
	}
	return r, err
}
func (b *uncertain) Revoke(ctx context.Context, a session.Address, h session.Digest) (bool, error) {
	r, err := b.Backend.Revoke(ctx, a, h)
	if err == nil && b.revoke {
		b.calls.Add(1)
		return false, b.cause
	}
	return r, err
}
func TestSessionLostMutationAcknowledgementNeverRetries(t *testing.T) {
	for _, operation := range []string{"create", "rotate", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			s := prepare(t)
			cause := errors.New("lost acknowledgement")
			wrapper := &uncertain{Backend: s.backend, cause: cause, create: operation == "create", rotate: operation == "rotate", revoke: operation == "revoke"}
			store, err := session.NewStore(wrapper, s.config)
			if err != nil {
				t.Fatal(err)
			}
			sessions, err := session.New(store, "members.web", s.provider, "web.session")
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "create":
				proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
				if err != nil {
					t.Fatal(err)
				}
				got, err := sessions.Issue(t.Context(), proof, session.IssueOptions{})
				if !errors.Is(err, cause) || !got.Secret().IsZero() {
					t.Fatal("unknown issue exposed secret", err)
				}
				if len(stored(t, s)) != 1 {
					t.Fatal("creation was retried or missing")
				}
			case "rotate":
				original := issue(t, s.sessions, 7, auth.Authenticated, false)
				got, err := sessions.Rotate(t.Context(), original.Secret())
				if !errors.Is(err, cause) || !got.Secret().IsZero() {
					t.Fatal("unknown rotation exposed secret", err)
				}
				if _, err := authenticate(t, s.sessions, original.Secret()); !errors.Is(err, auth.Unauthenticated) {
					t.Fatal("rotation did not actually commit", err)
				}
			case "revoke":
				original := issue(t, s.sessions, 7, auth.Authenticated, false)
				removed, err := sessions.Revoke(t.Context(), original.Secret())
				if !errors.Is(err, cause) || removed {
					t.Fatal(err)
				}
				if len(stored(t, s)) != 0 {
					t.Fatal("revocation did not commit")
				}
			}
			if wrapper.calls.Load() != 1 {
				t.Fatal("unknown operation retried", wrapper.calls.Load())
			}
		})
	}
}

func TestSessionRevokeIDRequiresMatchingSubjectAndGuard(t *testing.T) {
	s := prepare(t)
	first := issue(t, s.sessions, 7, auth.Authenticated, false)
	second := issue(t, s.sessions, 7, auth.Authenticated, false)
	rows, err := s.sessions.List(t.Context(), member{ID: 7}.reference())
	if err != nil || len(rows) != 2 {
		t.Fatal("list sessions", err)
	}
	id, err := session.ParseID[member](first.Info().ID().String())
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := s.sessions.RevokeID(t.Context(), member{ID: 8}.reference(), id); err != nil || removed {
		t.Fatal("revoked foreign subject", err)
	}
	other, err := session.New(s.store, "members.other", s.provider, "other.session")
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := other.RevokeID(t.Context(), member{ID: 7}.reference(), id); err != nil || removed {
		t.Fatal("revoked foreign guard", err)
	}
	if removed, err := s.sessions.RevokeID(t.Context(), member{ID: 7}.reference(), id); err != nil || !removed {
		t.Fatal("revoke session", err)
	}
	if removed, err := s.sessions.RevokeID(t.Context(), member{ID: 7}.reference(), id); err != nil || removed {
		t.Fatal("revocation is not idempotent", err)
	}
	if _, err := authenticate(t, s.sessions, first.Secret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("revoked credential accepted", err)
	}
	if _, err := authenticate(t, s.sessions, second.Secret()); err != nil {
		t.Fatal("sibling session revoked", err)
	}
}

func TestSessionCorruptSubjectFailsClosed(t *testing.T) {
	s := prepare(t)
	original := issue(t, s.sessions, 7, auth.Authenticated, false)
	row := stored(t, s)[0]
	identity, err := member{ID: 8}.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	data, err := value.NewJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+s.schema+`"`); err != nil {
			return err
		}
		_, err := sessionstore.QueryFoundrySessionSubjects().Update(t.Context(), tx, row.SubjectKey, sessionstore.SubjectDraft{}.SetIdentity(data))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := authenticate(t, s.sessions, original.Secret()); err == nil || got != (member{}) {
		t.Fatal("corrupted subject authenticated")
	}
	if rows, err := s.sessions.List(t.Context(), member{ID: 7}.reference()); err == nil || rows != nil {
		t.Fatal("corrupted subject listed")
	}
}

// Hold the same generated subject lock as the backend. Time advances while the
// request waits; its pre-lock lifetime must never authorize a post-expiry lookup.
func TestSessionExpiryIsCheckedAfterAcquiringSubjectLock(t *testing.T) {
	s := prepare(t)
	original := issue(t, s.sessions, 7, auth.Authenticated, false)
	row := stored(t, s)[0]
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
			if _, err := sessionstore.QueryFoundrySessionSubjects().ForUpdate().Find(t.Context(), tx, row.SubjectKey); err != nil {
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
		t.Fatal("subject lock failed", err)
	}
	done := make(chan error, 1)
	go func() { _, err := authenticate(t, s.sessions, original.Secret()); done <- err }()
	select {
	case err := <-done:
		t.Fatal("lookup escaped subject lock", err)
	case <-time.After(20 * time.Millisecond):
	}
	s.clock.Advance(11 * time.Minute)
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("expired credential authenticated after wait", err)
	}
}

func TestSessionUsesConfiguredSchemaBeforeTemporaryTables(t *testing.T) {
	s := prepare(t, func(c *databasepg.Config) { c.Pool.MaxOpen = 1; c.Pool.MaxIdle = 1 })
	for _, statement := range []string{`CREATE TEMP TABLE foundry_sessions (unrelated text)`, `CREATE TEMP TABLE foundry_session_subjects (unrelated text)`} {
		if _, err := s.db.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	issued := issue(t, s.sessions, 7, auth.Authenticated, false)
	if got, err := authenticate(t, s.sessions, issued.Secret()); err != nil || got.ID != 7 {
		t.Fatal("temporary table shadowed authoritative schema", err)
	}
}

func TestSessionMissingMigrationsAreErrorsNotAbsentCredentials(t *testing.T) {
	s := prepare(t)
	original := issue(t, s.sessions, 7, auth.Authenticated, false)
	config := sessionpg.DefaultConfig()
	config.Schema = pgtest.Namespace(t, s.db)
	backend, err := sessionpg.New(s.db, config)
	if err != nil {
		t.Fatal("constructor unexpectedly used database", err)
	}
	store, err := session.NewStore(backend, s.config)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(store, "members.web", s.provider, "web.session")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := authenticate(t, sessions, original.Secret()); err == nil || errors.Is(err, auth.Unauthenticated) || got != (member{}) {
		t.Fatal("database failure became absent credential", err)
	}
}

type nilClock func() time.Time

func (f nilClock) Now() time.Time { return f() }
func TestSessionPostgresRejectsTypedNilClock(t *testing.T) {
	config := sessionpg.DefaultConfig()
	config.Clock = nilClock(nil)
	if err := config.Validate(); err == nil {
		t.Fatal("typed nil clock accepted")
	}
}
