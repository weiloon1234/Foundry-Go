package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresConnectivity(t *testing.T) {
	config := pgtest.Config(t)
	db := pgtest.Open(t)
	var selectedDatabase, selectedUser, version, timezone string
	err := database.ScanOne(t.Context(), db, "SELECT current_database(), current_user, current_setting('server_version'), current_setting('TimeZone')", nil, &selectedDatabase, &selectedUser, &version, &timezone)
	if err != nil {
		t.Fatal(err)
	}
	if selectedDatabase != config.Database || selectedUser != config.User || timezone != "UTC" {
		t.Fatal("PostgreSQL connection did not select the intended project identity and session timezone")
	}
	t.Logf("Verified PostgreSQL %s with the isolated project account", version)
}

func execute(t *testing.T, db database.Executor, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, db database.Executor, table string) int64 {
	t.Helper()
	var count int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM "+table, nil, &count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPostgresParametersStreamingAndResourceRelease(t *testing.T) {
	db := pgtest.Open(t, func(c *postgres.Config) {
		c.Pool.MaxOpen = 1
		c.Pool.MaxIdle = 1
		c.Pool.AcquireTimeout = 25 * time.Millisecond
	})
	const input = "quote' unicode λ and SQL-looking '; SELECT 1; --"
	var text, decimal string
	var instant time.Time
	wantedTime := time.Date(2026, 9, 11, 7, 8, 9, 123456000, time.UTC)
	err := database.ScanOne(t.Context(), db, "SELECT $1::text, $2::numeric::text, $3::timestamptz", []any{input, "12345678901234567890.12345678", wantedTime}, &text, &decimal, &instant)
	if err != nil || text != input || decimal != "12345678901234567890.12345678" || !instant.Equal(wantedTime) {
		t.Fatalf("parameter/scan boundary: %v", err)
	}
	rows, err := db.Query(t.Context(), "SELECT generate_series(1,10000)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("stream had no first row")
	}
	if _, err := db.Exec(t.Context(), "SELECT 1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool acquisition bound: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	seen := 0
	err = database.ForEach(t.Context(), db, "SELECT generate_series(1,10000)", nil, func(row database.Row) (int64, error) { var value int64; err := row.Scan(&value); return value, err }, func(int64) error {
		seen++
		if seen == 10 {
			return io.EOF
		}
		return nil
	})
	if !errors.Is(err, io.EOF) || seen != 10 || db.Stats().InUse != 0 || db.Stats().Owners != 0 {
		t.Fatalf("early stream cleanup: %v", err)
	}
	rows, err = db.Query(t.Context(), "SELECT 'not an integer'::text")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing scan failure row")
	}
	var wrong int64
	if err := rows.Scan(&wrong); err == nil || db.Stats().InUse != 0 {
		t.Fatal("scan error leaked a connection")
	}
	if err := db.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresCancellationUnavailableAndProtocolBounds(t *testing.T) {
	db := pgtest.Open(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	_, err := db.Exec(ctx, "SELECT pg_sleep(10)")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query deadline: %v", err)
	}
	if err := db.Ping(t.Context()); err != nil {
		t.Fatal("pool could not recover after canceled query")
	}
	config := pgtest.Config(t)
	config.Host, config.Port = "127.0.0.1", 1
	config.Pool.ConnectTimeout = 100 * time.Millisecond
	if unavailable, err := postgres.Open(t.Context(), config); err == nil {
		unavailable.Close(t.Context())
		t.Fatal("unavailable endpoint opened")
	}
	bounded := pgtest.Open(t, func(c *postgres.Config) { c.MaxProtocolMessageBytes = 1024 })
	var text string
	if err := database.ScanOne(t.Context(), bounded, "SELECT repeat('x',8192)", nil, &text); err == nil {
		t.Fatal("protocol message limit ignored")
	}
	if bounded.Stats().Owners != 0 {
		t.Fatal("oversized protocol message retained pool ownership")
	}
}

func TestPostgresTransactionsSavepointsAndAfterCommit(t *testing.T) {
	db := pgtest.Open(t, func(c *postgres.Config) { c.Pool.MaxOpen = 1; c.Pool.MaxIdle = 1 })
	schema := pgtest.Namespace(t, db)
	table := `"` + schema + `".records`
	execute(t, db, "CREATE TABLE "+table+" (id bigint PRIMARY KEY)")
	cause := errors.New("domain validation failed")
	called := false
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), "INSERT INTO "+table+" VALUES ($1)", int64(1)); err != nil {
			return err
		}
		if err := tx.AfterCommit(func(context.Context) error { called = true; return nil }); err != nil {
			return err
		}
		return cause
	})
	// A confirmed rollback returns the application failure unchanged.
	var detail *database.Error
	if err != cause || errors.As(err, &detail) || called || countRows(t, db, table) != 0 {
		t.Fatalf("rollback: %v", err)
	}
	var callbacks []string
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), "INSERT INTO "+table+" VALUES (2)"); err != nil {
			return err
		}
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			if _, err := child.Exec(t.Context(), "INSERT INTO "+table+" VALUES (3)"); err != nil {
				return err
			}
			if err := child.AfterCommit(func(context.Context) error { callbacks = append(callbacks, "discarded"); return nil }); err != nil {
				return err
			}
			return cause
		}); !errors.Is(err, cause) {
			return fmt.Errorf("savepoint did not fail as expected: %w", err)
		}
		if err := tx.Savepoint(t.Context(), func(child *database.Tx) error {
			if _, err := child.Exec(t.Context(), "INSERT INTO "+table+" VALUES (4)"); err != nil {
				return err
			}
			return child.AfterCommit(func(context.Context) error { callbacks = append(callbacks, "child"); return nil })
		}); err != nil {
			return err
		}
		return tx.AfterCommit(func(ctx context.Context) error {
			var count int64
			if err := database.ScanOne(ctx, db, "SELECT count(*) FROM "+table, nil, &count); err != nil {
				return err
			}
			if count != 2 {
				return errors.New("after-commit query saw incorrect persisted rows")
			}
			callbacks = append(callbacks, "outer")
			return nil
		})
	})
	if err != nil || !reflect.DeepEqual(callbacks, []string{"child", "outer"}) || countRows(t, db, table) != 2 {
		t.Fatalf("savepoint/after-commit sequence: %v %v", callbacks, err)
	}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := tx.Exec(t.Context(), "INSERT INTO "+table+" VALUES (5)")
		return err
	}, database.TxOptions{ReadOnly: true})
	if !errors.As(err, &detail) || detail.SQLState() != "25006" || detail.Outcome() != database.RolledBack {
		t.Fatalf("read-only transaction: %v", err)
	}
}

func TestPostgresConstraintDetailsAndCommitRejection(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	prefix := `"` + schema + `".`
	execute(t, db, "CREATE TABLE "+prefix+"parents (id bigint PRIMARY KEY)")
	execute(t, db, "CREATE TABLE "+prefix+"records (id bigint PRIMARY KEY, parent_id bigint CONSTRAINT records_parent_fk REFERENCES "+prefix+"parents(id), name text NOT NULL, amount numeric CONSTRAINT positive_amount CHECK(amount>=0))")
	execute(t, db, "INSERT INTO "+prefix+"records(id,name,amount) VALUES(1,'initial',1)")
	for _, test := range []struct {
		sql               string
		code              database.Code
		state, constraint string
	}{
		{"INSERT INTO " + prefix + "records(id,name) VALUES(1,'duplicate')", database.UniqueViolation, "23505", "records_pkey"},
		{"INSERT INTO " + prefix + "records(id,name,parent_id) VALUES(2,'orphan',99)", database.ForeignKeyViolation, "23503", "records_parent_fk"},
		{"INSERT INTO " + prefix + "records(id,name,amount) VALUES(3,'negative',-1)", database.CheckViolation, "23514", "positive_amount"},
		{"INSERT INTO " + prefix + "records(id) VALUES(4)", database.NotNullViolation, "23502", ""},
	} {
		_, err := db.Exec(t.Context(), test.sql)
		var detail *database.Error
		if !errors.Is(err, test.code) || !errors.As(err, &detail) || detail.SQLState() != test.state || detail.Constraint() != test.constraint {
			t.Fatalf("constraint classification: %v", err)
		}
	}
	deferred := prefix + "deferred_records"
	execute(t, db, "CREATE TABLE "+deferred+" (value text CONSTRAINT deferred_unique UNIQUE DEFERRABLE INITIALLY DEFERRED)")
	called := false
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), "INSERT INTO "+deferred+" VALUES('same'),('same')"); err != nil {
			return err
		}
		return tx.AfterCommit(func(context.Context) error { called = true; return nil })
	})
	var detail *database.Error
	if !errors.Is(err, database.UniqueViolation) || !errors.As(err, &detail) || detail.Outcome() != database.RolledBack || called || countRows(t, db, deferred) != 0 {
		t.Fatalf("deferred commit rejection: %v", err)
	}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error { _, _ = tx.Exec(t.Context(), "SELECT 1/0"); return nil })
	if !errors.As(err, &detail) || detail.Outcome() != database.RolledBack {
		t.Fatalf("server ROLLBACK response to commit was not classified: %v", err)
	}
}

func TestPostgresSerializableConflict(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	table := `"` + schema + `".counter`
	execute(t, db, "CREATE TABLE "+table+" (id bigint PRIMARY KEY,value bigint)")
	execute(t, db, "INSERT INTO "+table+" VALUES(1,0)")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ready, proceed := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- db.Transaction(ctx, func(tx *database.Tx) error {
			var value int64
			if err := database.ScanOne(ctx, tx, "SELECT value FROM "+table+" WHERE id=1", nil, &value); err != nil {
				return err
			}
			close(ready)
			select {
			case <-proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := tx.Exec(ctx, "UPDATE "+table+" SET value=2 WHERE id=1")
			return err
		}, database.TxOptions{Isolation: database.Serializable})
	}()
	select {
	case <-ready:
	case err := <-result:
		t.Fatalf("first transaction: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	err := db.Transaction(ctx, func(tx *database.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE "+table+" SET value=1 WHERE id=1")
		return err
	}, database.TxOptions{Isolation: database.Serializable})
	close(proceed)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, database.SerializationFailure) {
		t.Fatalf("serialization conflict classification: %v", err)
	}
}

func TestPostgresDeadlockClassification(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	table := `"` + schema + `".locks`
	execute(t, db, "CREATE TABLE "+table+" (id bigint PRIMARY KEY,value bigint)")
	execute(t, db, "INSERT INTO "+table+" VALUES(1,0),(2,0)")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	proceed := make(chan struct{})
	results := make(chan error, 2)
	for _, first := range []int64{1, 2} {
		go func() {
			results <- db.Transaction(ctx, func(tx *database.Tx) error {
				if _, err := tx.Exec(ctx, "UPDATE "+table+" SET value=value+1 WHERE id=$1", first); err != nil {
					return err
				}
				ready <- struct{}{}
				select {
				case <-proceed:
				case <-ctx.Done():
					return ctx.Err()
				}
				_, err := tx.Exec(ctx, "UPDATE "+table+" SET value=value+1 WHERE id=$1", int64(3)-first)
				return err
			})
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case err := <-results:
			close(proceed)
			t.Fatalf("deadlock setup: %v", err)
		case <-ctx.Done():
			close(proceed)
			t.Fatal(ctx.Err())
		}
	}
	close(proceed)
	failures := 0
	for range 2 {
		err := <-results
		if errors.Is(err, database.Deadlock) {
			failures++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if failures != 1 {
		t.Fatal("expected exactly one deadlock victim")
	}
}

func TestPostgresMigrationsConcurrencyDriftRecoveryAndSeeding(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	table := `"` + schema + `".records`
	initial := migrate.Key{Origin: "app", ID: "001_records"}
	definitions := []migrate.Definition{
		{Key: initial, Version: "v0.1.0", SQL: []string{"CREATE TABLE " + table + " (id bigint PRIMARY KEY)", "SELECT pg_sleep(0.05)"}},
		{Key: migrate.Key{Origin: "app", ID: "002_label"}, Version: "v0.1.0", Requires: []migrate.Key{initial}, SQL: []string{"ALTER TABLE " + table + " ADD COLUMN label text NOT NULL DEFAULT ''"}},
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema = schema
	makeRunner := func(definitions []migrate.Definition) *migrate.Postgres {
		registry, err := migrate.New(definitions...)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := migrate.NewPostgres(db, registry, config)
		if err != nil {
			t.Fatal(err)
		}
		return runner
	}
	runner := makeRunner(definitions)
	status, err := runner.Status(t.Context())
	if err != nil || len(status.Statuses) != 2 || status.Statuses[0].State != migrate.Pending {
		t.Fatalf("initial read-only status: %v", err)
	}
	var historyCount int64
	if err := database.ScanOne(t.Context(), db, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname=$1 AND tablename=$2", []any{schema, config.Table}, &historyCount); err != nil || historyCount != 0 {
		t.Fatal("status created history table")
	}
	type outcome struct {
		result migrate.RunResult
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() { result, err := runner.Up(t.Context()); results <- outcome{result, err} }()
	}
	applied := 0
	for range 2 {
		item := <-results
		if item.err != nil {
			t.Fatal(item.err)
		}
		applied += len(item.result.Applied)
	}
	if applied != 2 {
		t.Fatal("concurrent migrations applied more than once")
	}
	status, err = runner.Status(t.Context())
	if err != nil || status.Check() != nil || status.LastBatch != 1 {
		t.Fatalf("committed history: %v", err)
	}
	changed := append([]migrate.Definition(nil), definitions...)
	changed[0].SQL = []string{"changed history"}
	if _, err := makeRunner(changed).Up(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatalf("history drift accepted: %v", err)
	}
	third := migrate.Definition{Key: migrate.Key{Origin: "app", ID: "003_revision"}, Version: "v0.1.0", SQL: []string{"SELECT foundry_nonexistent_acceptance_function()"}}
	pending := append(append([]migrate.Definition(nil), definitions...), third)
	result, err := makeRunner(pending).Up(t.Context())
	if err == nil || result.Interrupted == nil || len(result.Applied) != 0 {
		t.Fatalf("failed migration outcome: %+v %v", result, err)
	}
	pending[2].SQL = []string{"ALTER TABLE " + table + " ADD COLUMN revision bigint NOT NULL DEFAULT 0"}
	result, err = makeRunner(pending).Up(t.Context())
	if err != nil || len(result.Applied) != 1 || result.Batch != 2 {
		t.Fatalf("unapplied migration recovery: %+v %v", result, err)
	}
	seeders, err := seed.New(seed.Definition{ID: "app.records", Run: func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO "+table+" (id,label) VALUES($1,$2) ON CONFLICT(id) DO NOTHING", int64(1), "seeded")
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := seeders.Run(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	if countRows(t, db, table) != 1 {
		t.Fatal("explicit repeat-safe seeder duplicated data")
	}
}
