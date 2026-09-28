package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	pg "github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"strings"
	"testing"
)

func testScope() (pg.Config, infrastructure.DatabaseSettings) {
	c := pg.DefaultConfig()
	c.Host, c.Database, c.User, c.Schema = "127.0.0.1", "test", "test", "retained_scope"
	c.Password = secret.New("private-credential")
	c.Pool.MaxOpen, c.Pool.MaxIdle = 2, 2
	s := infrastructure.DefaultDatabaseSettings()
	s.Default = "main"
	conn := infrastructure.DefaultConnectionSettings()
	conn.ReadEnabled = true
	conn.Read.Host = "never-contact-replica"
	s.Connections = infrastructure.DatabaseConnections{"main": conn, "audit": conn}
	return c, s
}
func TestScopeBindingCopiesNamedDefaultsAndReadSelection(t *testing.T) {
	scope, original := testScope()
	env, err := Bind(original, selection(scope, "main"), selection(scope, "audit").WithReads())
	if err != nil {
		t.Fatal(err)
	}
	s := env.Settings()
	if s.Default != "main" || s.Connections["main"].ReadEnabled || !s.Connections["audit"].ReadEnabled || s.Connections["audit"].Read.Host != scope.Host || s.Connections["audit"].Read.Schema != scope.Schema {
		t.Fatal("read routing/default was not explicit")
	}
	delete(s.Connections, "main")
	if len(env.Settings().Connections) != 2 || original.Connections["main"].Primary.Schema != "" {
		t.Fatal("scope mutated caller or exposed owned maps")
	}
	for _, value := range []any{scope, selection(scope, "main"), *env} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			if strings.Contains(fmt.Sprintf(format, value), "private-credential") {
				t.Fatal("scope diagnostic leaked credentials")
			}
		}
	}
	for _, selected := range [][]Selection{nil, {selection(scope, "main")}, {selection(scope, "main", "main", "audit")}, {selection(scope, "unknown")}, {nilScopeSelection()}} {
		if _, err := Bind(original, selected...); err == nil {
			t.Fatal("invalid scope selection accepted")
		}
	}
	original.MaxConnections = 1
	if _, err := Bind(original, selection(scope, "main", "audit")); !errors.Is(err, fault.Invalid) {
		t.Fatal("connection ceiling ignored")
	}
}
func nilScopeSelection() Selection { var s *pgtest.Scope; return On(s, "main", "audit") }
func selection(c pg.Config, names ...database.ConnectionName) Selection {
	return Selection{config: c, names: names}
}

func TestScopeMigrationTargetsShareOnlyIdenticalDefinitions(t *testing.T) {
	scope, s := testScope()
	env, err := Bind(s, selection(scope, "main", "audit"))
	if err != nil {
		t.Fatal(err)
	}
	definition := migrate.Definition{Key: migrate.Key{Origin: "test", ID: "001"}, Version: "v1", SQL: []string{"CREATE TABLE records(id bigint)"}}
	targets := []infrastructure.MigrationTarget{{Definitions: []migrate.Definition{definition}}, {Connection: "audit", Definitions: []migrate.Definition{definition}}}
	groups, err := env.migrationGroups(targets)
	if err != nil || len(groups) != 1 || len(groups[0].registry.Entries()) != 1 {
		t.Fatal("shared schema did not group migrations", err)
	}
	targets[1].Definitions = []migrate.Definition{{Key: definition.Key, Version: "v2", SQL: definition.SQL}}
	if _, err := env.migrationGroups(targets); !errors.Is(err, fault.Conflict) {
		t.Fatal("conflicting migration accepted", err)
	}
	targets[1] = infrastructure.MigrationTarget{Schema: "public"}
	if _, err := env.migrationGroups(targets); !errors.Is(err, fault.Invalid) {
		t.Fatal("migration escaped namespace", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := env.Migrate(ctx, targets...); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled migration performed work", err)
	}
	if err := env.Migrate(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil migration context accepted")
	}
}
