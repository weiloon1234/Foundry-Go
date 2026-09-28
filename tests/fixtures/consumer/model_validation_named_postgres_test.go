package consumer_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/validation"
	databasevalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

// Hold real query rows so the barrier proves concurrent pool ownership, not
// merely overlapping callbacks before any database operation has started.
type heldValidationExecutor struct {
	database.Executor
	name    database.ConnectionName
	entered chan<- database.ConnectionName
	release <-chan struct{}
}

func (e heldValidationExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	rows, err := e.Executor.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	select {
	case e.entered <- e.name:
	case <-ctx.Done():
		return nil, errors.Join(ctx.Err(), rows.Close())
	}
	select {
	case <-e.release:
		return rows, nil
	case <-ctx.Done():
		return nil, errors.Join(ctx.Err(), rows.Close())
	}
}

func validationConnections(t *testing.T) *database.Connections {
	t.Helper()
	var entries []database.Connection
	for _, item := range []struct {
		name database.ConnectionName
		code models.CountryCode
	}{{"accounts", "MY"}, {"legacy", "SG"}} {
		db := pgtest.Isolate(t).Open(t)
		if _, err := db.Exec(t.Context(), `CREATE TABLE countries (code text PRIMARY KEY, name text NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(t.Context(), `INSERT INTO countries (code, name) VALUES ($1, 'Test country')`, string(item.code)); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, database.Connection{Name: item.name, Value: db})
	}
	connections, err := database.NewConnections("accounts", entries...)
	if err != nil {
		t.Fatal(err)
	}
	return connections
}

func validationConnection(t *testing.T, connections *database.Connections, name database.ConnectionName) *database.DB {
	t.Helper()
	db, err := connections.Connection(name)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresParallelValidationKeepsNamedConnections(t *testing.T) {
	connections := validationConnections(t)
	accounts := validationConnection(t, connections, "accounts")
	legacy := validationConnection(t, connections, "legacy")
	field := models.CountryFields().Code
	entered, release := make(chan database.ConnectionName, 2), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rules := validation.Parallel(
		databasevalidation.Unique(heldValidationExecutor{accounts, "accounts", entered, release}, models.QueryCountries(), field),
		databasevalidation.Exists(heldValidationExecutor{legacy, "legacy", entered, release}, models.QueryCountries(), field),
	)
	done := make(chan error, 1)
	go func() { done <- rules.Check(ctx, models.CountryCode("SG"), validation.DefaultLimits()) }()
	seen := make(map[database.ConnectionName]bool)
	for range 2 {
		select {
		case name := <-entered:
			seen[name] = true
		case <-ctx.Done():
			unblock()
			<-done
			t.Fatal("named database checks did not overlap")
		}
	}
	unblock()
	if err := <-done; err != nil || len(seen) != 2 {
		t.Fatal("named executor identity was lost", err, seen)
	}

	// A custom rule can borrow both named pools while retaining the field type.
	custom := validation.Custom(validation.Spec{ID: "consumer.available_legacy_country", Message: "Country is unavailable."}, func(ctx context.Context, code models.CountryCode) (bool, error) {
		used, err := models.QueryCountries().Where(field.Eq(code)).Exists(ctx, accounts)
		if err != nil || used {
			return false, err
		}
		return models.QueryCountries().Where(field.Eq(code)).Exists(ctx, legacy)
	})
	batch := databasevalidation.ExistsAll(legacy, models.QueryCountries(), field)
	for _, code := range []models.CountryCode{"SG", "MY"} {
		for _, err := range []error{custom.Check(t.Context(), code, validation.DefaultLimits()), batch.Check(t.Context(), []models.CountryCode{code, code}, validation.DefaultLimits())} {
			if code == "SG" && err != nil {
				t.Fatal(err)
			}
			var rejected *validation.Errors
			if code == "MY" && !errors.As(err, &rejected) {
				t.Fatal("wrong named pool accepted input", err)
			}
		}
	}
	combined := validation.Parallel(custom, databasevalidation.Unique(accounts, models.QueryCountries(), field), databasevalidation.Exists(legacy, models.QueryCountries(), field))
	if err := combined.Check(t.Context(), "SG", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	// A failed selected pool must never fall back to another connection or turn
	// an unavailable lookup into an ordinary validation rejection/success.
	if err := legacy.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	var rejected *validation.Errors
	if err := combined.Check(t.Context(), "SG", validation.DefaultLimits()); !errors.Is(err, fault.Internal) || errors.As(err, &rejected) {
		t.Fatal("database failure was treated as a validation observation", err)
	}
	if err := databasevalidation.Exists(accounts, models.QueryCountries(), field).Check(t.Context(), "MY", validation.DefaultLimits()); err != nil {
		t.Fatal("failure affected the independent connection", err)
	}
}

func TestPostgresParallelValidationCancellationReleasesNamedPools(t *testing.T) {
	connections := validationConnections(t)
	accounts := validationConnection(t, connections, "accounts")
	legacy := validationConnection(t, connections, "legacy")
	entered, release := make(chan database.ConnectionName, 2), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	field := models.CountryFields().Code
	rules := validation.Parallel(
		databasevalidation.Unique(heldValidationExecutor{accounts, "accounts", entered, release}, models.QueryCountries(), field),
		databasevalidation.Exists(heldValidationExecutor{legacy, "legacy", entered, release}, models.QueryCountries(), field),
	)
	done := make(chan error, 1)
	go func() { done <- rules.Check(ctx, models.CountryCode("SG"), validation.DefaultLimits()) }()
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			<-done
			t.Fatal("named checks did not acquire their pools")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("request cancellation lost", err)
	}
	close(release)
	// Both original rule values and both pools remain usable after cancellation.
	if err := rules.Check(t.Context(), "SG", validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}
