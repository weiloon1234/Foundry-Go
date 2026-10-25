package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestTypeRegistryLoadTypeWithShadowedStartupSearchPath(t *testing.T) {
	// This same-package fixture needs native startup parameters that Config.Schema
	// deliberately does not expose. Do not change shared role or database settings.
	url := os.Getenv("FOUNDRY_TEST_POSTGRES_URL")
	if url == "" {
		if os.Getenv("FOUNDRY_TEST_POSTGRES_REQUIRED") == "1" {
			t.Fatal("required PostgreSQL acceptance URL is missing")
		}
		t.Skip("PostgreSQL acceptance is opt-in; run make test-postgres")
	}
	config, err := ParseURL(secret.New(url))
	if err != nil {
		t.Fatal("parse PostgreSQL test configuration failed")
	}
	nativeConfig, err := connectionConfig(config)
	if err != nil {
		t.Fatal("construct PostgreSQL test configuration failed")
	}
	connect := func(t *testing.T, config *pgx.ConnConfig) *pgx.Conn {
		t.Helper()
		conn, err := pgx.ConnectConfig(t.Context(), config)
		if err != nil {
			t.Fatal("connect PostgreSQL registry fixture failed")
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := conn.Close(ctx); err != nil {
				t.Error("close PostgreSQL registry fixture failed")
			}
		})
		return conn
	}
	setup := connect(t, nativeConfig)
	schema := "foundry_test_" + strings.ToLower(rand.Text())
	for _, statement := range []string{
		`CREATE SCHEMA "` + schema + `"`,
		`CREATE DOMAIN "` + schema + `".text AS pg_catalog.text CHECK (VALUE = 'shadow-domain-value')`,
		`CREATE TABLE "` + schema + `".pg_type (shadow pg_catalog.bool)`,
		`CREATE TYPE "` + schema + `".loaded_enum AS ENUM ('value')`,
	} {
		if _, err := setup.Exec(t.Context(), statement); err != nil {
			t.Fatal("create retained PostgreSQL registry fixture failed")
		}
	}
	originalPath := `"` + schema + `", pg_catalog`
	nativeConfig.RuntimeParams["search_path"] = originalPath
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "loaded", true: "failed_load_restores_path"}[failure], func(t *testing.T) {
			conn := connect(t, nativeConfig)
			err := initializeTypeRegistry(t.Context(), conn, func(ctx context.Context, registry *TypeRegistry) error {
				if failure {
					// The enum array cannot load before its element is registered. Swallowing
					// this native discovery failure must still prevent connection admission.
					_, _ = registry.LoadType(ctx, schema+"._loaded_enum")
					return nil
				}
				for _, name := range []string{schema + ".loaded_enum", "loaded_enum"} {
					typ, err := registry.LoadType(ctx, name)
					if err != nil {
						return err
					}
					if typ.Name != name {
						return errors.New("loaded type name changed")
					}
					if err := registry.RegisterType(typ); err != nil {
						return err
					}
				}
				return nil
			})
			if (err != nil) != failure {
				t.Fatal("type discovery did not preserve success/failure under shadowing path")
			}
			path := conn.PgConn().ExecParams(t.Context(), "SELECT pg_catalog.current_setting('search_path')", nil, nil, nil, nil).Read()
			if path.Err != nil || len(path.Rows) != 1 || len(path.Rows[0]) != 1 || string(path.Rows[0][0]) != originalPath {
				t.Fatal("type discovery did not restore startup search_path")
			}
			if !failure {
				qualified, qok := conn.TypeMap().TypeForName(schema + ".loaded_enum")
				unqualified, uok := conn.TypeMap().TypeForName("loaded_enum")
				if !qok || !uok || qualified.OID != unqualified.OID {
					t.Fatal("discovery changed original-path name resolution")
				}
			}
		})
	}
}
