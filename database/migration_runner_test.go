package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// This protocol fixture verifies runner sequencing and failure handling. It is
// deliberately not a PostgreSQL compatibility or durability test.
type migrationServer struct {
	progressTable         bool
	progress              map[migrate.Key]migrate.Progress
	pendingProgressDelete map[int]migrate.Key
	loseStatement         string
	mu                    sync.Mutex
	table                 bool
	locks                 map[int64]int
	history               []migrate.Applied
	inTransaction         map[int]bool
	pending               map[int][]migrate.Applied
	statements            map[int][]string
	committedSQL          []string
	ddl                   []string
	failStatement         string
	failUnlock            bool
	loseCommit            bool
	waitStarted           chan struct{}
	proceed               chan struct{}
	contended             chan struct{}
	startOnce             sync.Once
	contentionOnce        sync.Once
}

func newMigrationServer() *migrationServer {
	return &migrationServer{progress: make(map[migrate.Key]migrate.Progress), pendingProgressDelete: make(map[int]migrate.Key), locks: make(map[int64]int), inTransaction: make(map[int]bool), pending: make(map[int][]migrate.Applied), statements: make(map[int][]string)}
}

func (s *migrationServer) state(owner int) *driverState {
	state := &driverState{}
	state.closeHook = func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		for key, holder := range s.locks {
			if holder == owner {
				delete(s.locks, key)
			}
		}
		delete(s.inTransaction, owner)
		delete(s.pending, owner)
		delete(s.statements, owner)
		return nil
	}
	state.begin = func(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.inTransaction[owner] {
			return nil, errors.New("nested physical transaction")
		}
		s.inTransaction[owner] = true
		return migrationTransaction{s, owner, state}, nil
	}
	state.exec = func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
		if statement == "WAIT" && s.waitStarted != nil {
			s.startOnce.Do(func() { close(s.waitStarted) })
			select {
			case <-s.proceed:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if statement == s.failStatement {
			s.failStatement = ""
			return nil, errors.New("injected migration statement failure")
		}
		if strings.HasPrefix(statement, "CREATE SCHEMA IF NOT EXISTS") || strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS") {
			s.ddl = append(s.ddl, statement)
			if strings.HasPrefix(statement, "CREATE TABLE") {
				if strings.Contains(statement, "_progress_") {
					s.progressTable = true
				} else {
					s.table = true
				}
			}
			return driver.RowsAffected(0), nil
		}
		if strings.HasPrefix(statement, "DELETE FROM") && strings.Contains(statement, "_progress_") {
			if !s.inTransaction[owner] {
				return nil, errors.New("progress finalized outside transaction")
			}
			key := migrate.Key{Origin: migrate.Origin(args[0].Value.(string)), ID: migrate.ID(args[1].Value.(string))}
			if _, ok := s.progress[key]; !ok {
				return driver.RowsAffected(0), nil
			}
			s.pendingProgressDelete[owner] = key
			return driver.RowsAffected(1), nil
		}
		if !s.inTransaction[owner] {
			if !s.progressTable {
				return nil, errors.New("migration SQL executed without transaction")
			}
			s.committedSQL = append(s.committedSQL, statement)
			if statement == s.loseStatement {
				s.loseStatement = ""
				return nil, errors.New("statement response lost after durable effect")
			}
			return driver.RowsAffected(1), nil
		}
		s.statements[owner] = append(s.statements[owner], statement)
		return driver.RowsAffected(1), nil
	}
	state.query = func(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows := &resultRows{state: state}
		switch {
		case strings.Contains(statement, "pg_try_advisory_lock"):
			key := args[0].Value.(int64)
			available := s.locks[key] == 0 || s.locks[key] == owner
			if available {
				s.locks[key] = owner
			} else if s.contended != nil {
				s.contentionOnce.Do(func() { close(s.contended) })
			}
			rows.values = [][]driver.Value{{available}}
		case strings.Contains(statement, "pg_advisory_unlock"):
			if s.failUnlock {
				return nil, errors.New("injected unlock failure")
			}
			key := args[0].Value.(int64)
			owned := s.locks[key] == owner
			if owned {
				delete(s.locks, key)
			}
			rows.values = [][]driver.Value{{owned}}
		case strings.Contains(statement, "pg_catalog.pg_class"):
			exists := s.table
			if strings.Contains(args[1].Value.(string), "_progress_") {
				exists = s.progressTable
			}
			if exists {
				rows.values = [][]driver.Value{{"r"}}
			}
		case strings.HasPrefix(statement, "SELECT origin,id,version"):
			rows.columns = []string{"origin", "id", "version", "checksum", "batch", "confirmed", "state", "revision", "updated_at"}
			for _, item := range s.progress {
				rows.values = append(rows.values, []driver.Value{string(item.Key.Origin), string(item.Key.ID), string(item.Version), item.Checksum.String(), item.Batch, int64(item.Confirmed), string(item.State), item.Revision, item.UpdatedAt})
			}
		case strings.HasPrefix(statement, "INSERT INTO") && strings.Contains(statement, "_progress_"):
			checksum, err := migrate.ParseChecksum(args[3].Value.(string))
			if err != nil {
				return nil, err
			}
			item := migrate.Progress{Key: migrate.Key{Origin: migrate.Origin(args[0].Value.(string)), ID: migrate.ID(args[1].Value.(string))}, Version: migrate.Version(args[2].Value.(string)), Checksum: checksum, Batch: args[4].Value.(int64), State: migrate.StepReady, Revision: 1, UpdatedAt: time.Now().UTC()}
			s.progress[item.Key] = item
			rows.values = [][]driver.Value{{item.UpdatedAt}}
		case strings.HasPrefix(statement, "UPDATE") && strings.Contains(statement, "_progress_"):
			key := migrate.Key{Origin: migrate.Origin(args[2].Value.(string)), ID: migrate.ID(args[3].Value.(string))}
			item, ok := s.progress[key]
			if ok && item.Revision == args[6].Value.(int64) && string(item.State) == args[7].Value.(string) {
				item.State = migrate.StepState(args[0].Value.(string))
				item.Confirmed = int(args[1].Value.(int64))
				item.Revision++
				item.UpdatedAt = time.Now().UTC()
				s.progress[key] = item
				rows.values = [][]driver.Value{{item.UpdatedAt}}
			}
		case strings.HasPrefix(statement, "SELECT origin, id, version"):
			rows.columns = []string{"origin", "id", "version", "checksum", "batch", "applied_at"}
			limit := int(args[0].Value.(int64))
			for _, entry := range s.history[:min(len(s.history), limit)] {
				rows.values = append(rows.values, []driver.Value{string(entry.Key.Origin), string(entry.Key.ID), string(entry.Version), entry.Checksum.String(), entry.Batch, entry.AppliedAt})
			}
		case strings.HasPrefix(statement, "INSERT INTO"):
			if !s.inTransaction[owner] {
				return nil, errors.New("history insert outside transaction")
			}
			checksum, err := migrate.ParseChecksum(args[3].Value.(string))
			if err != nil {
				return nil, err
			}
			entry := migrate.Applied{Key: migrate.Key{Origin: migrate.Origin(args[0].Value.(string)), ID: migrate.ID(args[1].Value.(string))}, Version: migrate.Version(args[2].Value.(string)), Checksum: checksum, Batch: args[4].Value.(int64), AppliedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
			s.pending[owner] = append(s.pending[owner], entry)
			rows.values = [][]driver.Value{{entry.AppliedAt}}
		default:
			return nil, fmt.Errorf("unexpected protocol statement: %s", statement)
		}
		return rows, nil
	}
	return state
}

type migrationTransaction struct {
	server *migrationServer
	owner  int
	state  *driverState
}

func (tx migrationTransaction) Commit() error {
	tx.state.committed.Add(1)
	s := tx.server
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, s.pending[tx.owner]...)
	if key, ok := s.pendingProgressDelete[tx.owner]; ok {
		delete(s.progress, key)
		delete(s.pendingProgressDelete, tx.owner)
	}
	s.committedSQL = append(s.committedSQL, s.statements[tx.owner]...)
	delete(s.pending, tx.owner)
	delete(s.statements, tx.owner)
	delete(s.inTransaction, tx.owner)
	if s.loseCommit {
		s.loseCommit = false
		return errors.New("commit response lost")
	}
	return nil
}
func (tx migrationTransaction) Rollback() error {
	tx.state.rolledBack.Add(1)
	s := tx.server
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingProgressDelete, tx.owner)
	delete(s.pending, tx.owner)
	delete(s.statements, tx.owner)
	delete(s.inTransaction, tx.owner)
	return nil
}

func migrationDefinitions() []migrate.Definition {
	first := migrate.Key{Origin: "app", ID: "0001_first"}
	return []migrate.Definition{{Key: first, Version: "v1", SQL: []string{"FIRST"}}, {Key: migrate.Key{Origin: "app", ID: "0002_second"}, Version: "v1", SQL: []string{"SECOND"}, Requires: []migrate.Key{first}}}
}

func migrationRunner(t *testing.T, server *migrationServer, owner int, definitions []migrate.Definition, configure func(*migrate.PostgresConfig)) (*migrate.Postgres, *driverState) {
	t.Helper()
	state := server.state(owner)
	db := open(t, state, func(c *database.PoolConfig) { c.MaxOpen = 1; c.MaxIdle = 1 })
	registry, err := migrate.New(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.LockPollInterval = time.Millisecond
	if configure != nil {
		configure(&config)
	}
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	return runner, state
}

func TestMigrationStatusIsReadOnlyAndUpAppliesExactlyOnce(t *testing.T) {
	server := newMigrationServer()
	runner, state := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	report, err := runner.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Statuses) != 2 || report.Statuses[0].State != migrate.Pending || len(server.ddl) != 0 || len(server.locks) != 0 {
		t.Fatal("status mutated database")
	}
	result, err := runner.Up(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch != 1 || len(result.Applied) != 2 || result.Interrupted != nil || state.committed.Load() != 2 || !reflect.DeepEqual(server.committedSQL, []string{"FIRST", "SECOND"}) || len(server.locks) != 0 {
		t.Fatalf("migration application: %+v", result)
	}
	report, err = runner.Status(t.Context())
	if err != nil || report.Check() != nil || report.Statuses[1].State != migrate.Complete {
		t.Fatal("committed status missing")
	}
	result, err = runner.Up(t.Context())
	if err != nil || result.Batch != 0 || len(result.Applied) != 0 || state.committed.Load() != 2 {
		t.Fatal("applied migration repeated")
	}
}

func TestMigrationFailureRetainsEarlierCommitsAndRetriesOnlyPending(t *testing.T) {
	server := newMigrationServer()
	server.failStatement = "SECOND"
	runner, state := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	result, err := runner.Up(t.Context())
	if err == nil || len(result.Applied) != 1 || result.Interrupted == nil || result.Interrupted.ID != "0002_second" || len(server.history) != 1 || state.rolledBack.Load() != 1 || len(server.locks) != 0 {
		t.Fatalf("partial run: %+v %v", result, err)
	}
	result, err = runner.Up(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch != 2 || len(result.Applied) != 1 || len(server.history) != 2 || !reflect.DeepEqual(server.committedSQL, []string{"FIRST", "SECOND"}) {
		t.Fatal("resume repeated committed work")
	}
}

func TestMigrationCommitAmbiguityRetainsUnconfirmedKeyAndReconcilesHistory(t *testing.T) {
	server := newMigrationServer()
	server.loseCommit = true
	runner, state := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	result, err := runner.Up(t.Context())
	if !errors.Is(err, database.CommitUnknown) || len(result.Applied) != 0 || result.Interrupted == nil || len(server.history) != 1 || state.closed.Load() != 1 || len(server.locks) != 0 {
		t.Fatalf("ambiguous commit: %+v %v", result, err)
	}
	result, err = runner.Up(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch != 2 || len(result.Applied) != 1 || !reflect.DeepEqual(server.committedSQL, []string{"FIRST", "SECOND"}) {
		t.Fatal("uncertain committed migration was blindly repeated")
	}
}

func TestMigrationDriftAndHistoryLimitsPreventFurtherWrites(t *testing.T) {
	server := newMigrationServer()
	runner, _ := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	changed := migrationDefinitions()
	changed[0].SQL[0] = "CHANGED"
	runner, _ = migrationRunner(t, server, 2, changed, nil)
	report, err := runner.Status(t.Context())
	if err != nil || len(report.Problems) == 0 {
		t.Fatal("status omitted drift")
	}
	if result, err := runner.Up(t.Context()); !errors.Is(err, fault.Conflict) || len(result.Applied) != 0 || len(server.committedSQL) != 2 {
		t.Fatal("drift allowed migration writes")
	}
	limited, _ := migrationRunner(t, server, 3, migrationDefinitions(), func(c *migrate.PostgresConfig) { c.MaxHistory = 1 })
	if _, err := limited.Status(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("history entry bound ignored")
	}
}

func TestMigrationUnlockFailureDiscardsLockedConnection(t *testing.T) {
	server := newMigrationServer()
	server.failUnlock = true
	runner, state := migrationRunner(t, server, 1, migrationDefinitions(), nil)
	result, err := runner.Up(t.Context())
	if err == nil || len(result.Applied) != 2 || result.Interrupted != nil || state.closed.Load() != 1 || len(server.locks) != 0 {
		t.Fatalf("unlock failure leaked lock or lost committed result: %+v %v", result, err)
	}
}

func TestMigrationConcurrentRunnersSerializeAndReloadHistory(t *testing.T) {
	server := newMigrationServer()
	server.waitStarted = make(chan struct{})
	server.proceed = make(chan struct{})
	server.contended = make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(server.proceed) }) })
	definitions := migrationDefinitions()
	definitions[0].SQL = []string{"WAIT"}
	first, _ := migrationRunner(t, server, 1, definitions, nil)
	second, _ := migrationRunner(t, server, 2, definitions, nil)
	type outcome struct {
		result migrate.RunResult
		err    error
	}
	one, two := make(chan outcome, 1), make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() { result, err := first.Up(ctx); one <- outcome{result, err} }()
	select {
	case <-server.waitStarted:
	case <-ctx.Done():
		t.Fatal("first runner did not reach migration")
	}
	go func() { result, err := second.Up(ctx); two <- outcome{result, err} }()
	select {
	case <-server.contended:
	case <-ctx.Done():
		t.Fatal("second runner did not contend for lock")
	}
	release.Do(func() { close(server.proceed) })
	a, b := <-one, <-two
	if a.err != nil || b.err != nil || len(a.result.Applied) != 2 || len(b.result.Applied) != 0 || len(server.history) != 2 || len(server.committedSQL) != 2 || len(server.locks) != 0 {
		t.Fatalf("concurrent runs: %+v %+v", a, b)
	}
}

func TestMigrationLockWaitHonorsTimeoutWithoutApplyingSchema(t *testing.T) {
	server := newMigrationServer()
	server.waitStarted = make(chan struct{})
	server.proceed = make(chan struct{})
	server.contended = make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(server.proceed) }) })
	definitions := migrationDefinitions()
	definitions[0].SQL = []string{"WAIT"}
	first, _ := migrationRunner(t, server, 1, definitions, nil)
	second, _ := migrationRunner(t, server, 2, definitions, func(c *migrate.PostgresConfig) { c.LockTimeout = 5 * time.Millisecond })
	done := make(chan error, 1)
	go func() { _, err := first.Up(t.Context()); done <- err }()
	<-server.waitStarted
	result, err := second.Up(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) || len(result.Applied) != 0 {
		t.Fatalf("lock deadline: %+v %v", result, err)
	}
	release.Do(func() { close(server.proceed) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(server.ddl) != 2 {
		t.Fatal("contending runner changed schema before acquiring lock")
	}
}

func TestMigrationConfigurationRejectsIdentifierAliasingAndInjection(t *testing.T) {
	for _, invalid := range []string{"", "schema.table", `name";SELECT 1`, strings.Repeat("x", 64)} {
		config := migrate.DefaultPostgresConfig()
		config.Table = invalid
		if !errors.Is(config.Validate(), fault.Invalid) {
			t.Fatal("unsafe or truncated identifier accepted")
		}
	}
}

func TestMigrationPlanCannotExceedItsOwnHistoryReadLimit(t *testing.T) {
	server := newMigrationServer()
	runner, _ := migrationRunner(t, server, 1, migrationDefinitions(), func(config *migrate.PostgresConfig) { config.MaxHistory = 1 })
	result, err := runner.Up(t.Context())
	if !errors.Is(err, fault.Invalid) || len(result.Applied) != 0 || len(server.history) != 0 || len(server.committedSQL) != 0 {
		t.Fatalf("runner produced history it cannot read: %+v %v", result, err)
	}
}

func TestNonTransactionalLostStatementRequiresExplicitReconciliation(t *testing.T) {
	server := newMigrationServer()
	server.loseStatement = "CONCURRENT_INDEX"
	definition := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "index"}, Version: "v1", Mode: migrate.NonTransactional, SQL: []string{"CONCURRENT_INDEX", "NEXT"}}
	runner, _ := migrationRunner(t, server, 1, []migrate.Definition{definition}, nil)
	result, err := runner.Up(t.Context())
	if !errors.Is(err, migrate.ErrReconciliationRequired) || result.Interrupted == nil || len(server.committedSQL) != 1 {
		t.Fatal("lost statement outcome hidden", result, err)
	}
	report, err := runner.Status(t.Context())
	if err != nil || report.Check() == nil || len(report.Statuses) != 1 || report.Statuses[0].Progress == nil {
		t.Fatal("uncertain checkpoint absent", err)
	}
	interrupted := *report.Statuses[0].Progress
	if interrupted.Confirmed != 0 || interrupted.State == migrate.StepReady {
		t.Fatal(interrupted)
	}
	if _, err := runner.Up(t.Context()); err == nil || len(server.committedSQL) != 1 {
		t.Fatal("uncertain statement automatically replayed")
	}
	ready, err := runner.Reconcile(t.Context(), interrupted, migrate.StatementApplied)
	if err != nil || ready.Confirmed != 1 || ready.State != migrate.StepReady {
		t.Fatal(ready, err)
	}
	if _, err := runner.Reconcile(t.Context(), interrupted, migrate.StatementNotApplied); !errors.Is(err, fault.Conflict) {
		t.Fatal("stale attestation accepted", err)
	}
	result, err = runner.Up(t.Context())
	if err != nil || len(result.Applied) != 1 || !reflect.DeepEqual(server.committedSQL, []string{"CONCURRENT_INDEX", "NEXT"}) || len(server.progress) != 0 {
		t.Fatal("confirmed SQL repeated or finalization lost", result, err)
	}
}
func TestNonTransactionalLostFinalizationDoesNotRepeatEffects(t *testing.T) {
	server := newMigrationServer()
	server.loseCommit = true
	definition := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "index"}, Version: "v1", Mode: migrate.NonTransactional, SQL: []string{"CONCURRENT_INDEX"}}
	runner, _ := migrationRunner(t, server, 1, []migrate.Definition{definition}, nil)
	if _, err := runner.Up(t.Context()); err == nil {
		t.Fatal("lost bookkeeping commit became success")
	}
	report, err := runner.Status(t.Context())
	if err != nil || report.Check() != nil || report.Statuses[0].State != migrate.Complete {
		t.Fatal("durable history not reconciled", err)
	}
	result, err := runner.Up(t.Context())
	if err != nil || len(result.Applied) != 0 || len(server.committedSQL) != 1 {
		t.Fatal("durable statement repeated", err)
	}
}

func TestNonTransactionalProgressRejectsMissingPrerequisiteHistory(t *testing.T) {
	server := newMigrationServer()
	server.loseStatement = "ONLINE"
	first := migrate.Key{Origin: "app", ID: "first"}
	second := migrate.Key{Origin: "app", ID: "second"}
	runner, _ := migrationRunner(t, server, 1, []migrate.Definition{
		{Key: first, Version: "v1", SQL: []string{"FIRST"}},
		{Key: second, Version: "v1", Mode: migrate.NonTransactional, Requires: []migrate.Key{first}, SQL: []string{"ONLINE"}},
	}, nil)
	if _, err := runner.Up(t.Context()); !errors.Is(err, migrate.ErrReconciliationRequired) {
		t.Fatal(err)
	}
	report, err := runner.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	interrupted := *report.Statuses[1].Progress
	// Simulate history loss only in this in-memory protocol fixture.
	server.mu.Lock()
	history := server.history
	server.history = nil
	server.mu.Unlock()
	report, err = runner.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	missing := false
	for _, problem := range report.Problems {
		if problem.Code == migrate.DependencyNotApplied && problem.Key == second {
			missing = true
		}
	}
	if !missing {
		t.Fatal("unfinished migration lost prerequisite validation")
	}
	if _, err := runner.Reconcile(t.Context(), interrupted, migrate.StatementApplied); !errors.Is(err, fault.Conflict) {
		t.Fatal("reconciled inconsistent history", err)
	}
	server.mu.Lock()
	server.history = history
	server.mu.Unlock()
	if _, err := runner.Reconcile(t.Context(), interrupted, migrate.StatementApplied); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	server.history = nil
	server.mu.Unlock()
	if _, err := runner.Up(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal("ready checkpoint reran missing prerequisite", err)
	}
	if !reflect.DeepEqual(server.committedSQL, []string{"FIRST", "ONLINE"}) {
		t.Fatal("history drift repeated effects", server.committedSQL)
	}
}
