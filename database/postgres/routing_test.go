package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresRoutedEndpointsRetainIndependentSessions(t *testing.T) {
	primary := pgtest.Config(t)
	primary.ApplicationName = "foundry-routing-primary"
	primary.Pool.MaxOpen, primary.Pool.MaxIdle = 2, 1
	read := primary
	read.ApplicationName = "foundry-routing-read"
	config := postgres.RoutingConfig{Primary: primary, Read: value.Set(read), MaxConnections: 4}
	db, err := postgres.OpenRouted(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	check := func(executor database.Executor, route bool, want string) error {
		var rows *database.Rows
		var err error
		const sql = "SELECT current_setting('application_name')"
		if route {
			rows, err = database.ReadQuery(t.Context(), executor, sql)
		} else {
			rows, err = executor.Query(t.Context(), sql)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		var name string
		if !rows.Next() {
			return errors.New("endpoint query returned no row")
		}
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name != want {
			return errors.New("query used the wrong PostgreSQL endpoint session")
		}
		return rows.Close()
	}
	if err := check(db, true, read.ApplicationName); err != nil {
		t.Fatal(err)
	}
	if err := check(db, false, primary.ApplicationName); err != nil {
		t.Fatal(err)
	}
	if err := check(db.Primary(), true, primary.ApplicationName); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error { return check(tx, true, primary.ApplicationName) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Session(t.Context(), func(session *database.Session) error { return check(session, true, primary.ApplicationName) }); err != nil {
		t.Fatal(err)
	}
	for _, health := range db.Health(t.Context()) {
		if health.Error != nil {
			t.Fatal("configured endpoint health failed", health.Role, health.Error)
		}
	}
	stats := db.RoutingStats()
	replica, configured := stats.Read.Get()
	if !configured || stats.MaxConnections != 4 || replica.Role != database.ReadPool || stats.Primary.Role != database.PrimaryPool || db.Stats().Owners != 0 {
		t.Fatal("PostgreSQL routing accounting failed")
	}
}

func TestPostgresRoutingConfigurationRejectsBoundsBeforeConnecting(t *testing.T) {
	primary := postgres.DefaultConfig()
	primary.Host, primary.User, primary.Database = "127.0.0.1", "unconnected", "unconnected"
	primary.TLS = postgres.DisableTLS
	read := primary
	for _, config := range []postgres.RoutingConfig{
		{Primary: primary},
		{Primary: primary, MaxConnections: 15},
		{Primary: primary, Read: value.Set(read), MaxConnections: 31},
	} {
		if !errors.Is(config.Validate(), fault.Invalid) {
			t.Fatal("invalid routing budget accepted")
		}
		if db, err := postgres.OpenRouted(t.Context(), config); !errors.Is(err, fault.Invalid) || db != nil {
			t.Fatal("invalid routing attempted connection", err)
		}
	}
	if err := (postgres.RoutingConfig{Primary: primary, Read: value.Set(read), MaxConnections: 32}).Validate(); err != nil {
		t.Fatal(err)
	}
}
