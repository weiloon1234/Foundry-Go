package postgres_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresMigrationSearchPathKeepsObjectsAndHistoryInTargetSchema(t *testing.T) {
	// One pooled connection proves the session path is restored for later reuse.
	db := pgtest.Open(t, func(c *postgres.Config) { c.Pool.MaxOpen, c.Pool.MaxIdle = 1, 1 })
	schema := pgtest.Namespace(t, db)
	registry, err := migrate.New(migrate.Definition{Key: migrate.Key{Origin: "app", ID: "001_unqualified"}, Version: "v1", SQL: []string{
		"CREATE TABLE search_path_records (id bigint PRIMARY KEY)",
		"INSERT INTO search_path_records (id) VALUES (1)",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err := database.ScanOne(t.Context(), db, "SELECT pg_catalog.current_setting('search_path')", nil, &before); err != nil {
		t.Fatal(err)
	}
	config := migrate.DefaultPostgresConfig()
	config.Schema, config.SearchPath = schema, schema
	runner, err := migrate.NewPostgres(db, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	var rows, history int64
	if err := scanCount(t, db, `SELECT count(*) FROM "`+schema+`".search_path_records`, &rows); err != nil || rows != 1 {
		t.Fatal("unqualified migration SQL left the target schema", rows, err)
	}
	if err := scanCount(t, db, `SELECT count(*) FROM "`+schema+`".schema_migrations`, &history); err != nil || history != 1 {
		t.Fatal("history is not beside the target schema", history, err)
	}
	var after string
	if err := database.ScanOne(t.Context(), db, "SELECT pg_catalog.current_setting('search_path')", nil, &after); err != nil || after != before {
		t.Fatalf("pooled search path was not restored: %q -> %q %v", before, after, err)
	}
}
