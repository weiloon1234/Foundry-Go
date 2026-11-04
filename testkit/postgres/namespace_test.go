package postgres

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/postgres"
)

func TestDropNamespaceRefusesNonGeneratedIdentifiers(t *testing.T) {
	for _, name := range []string{"", "public", "customer_schema", namespacePrefix, namespacePrefix + `bad"; DROP SCHEMA public;--`, namespacePrefix + strings.Repeat("a", 64)} {
		t.Run(name, func(t *testing.T) {
			if err := dropNamespace(postgres.Config{}, name); err == nil || err.Error() != "refusing to drop a non-generated PostgreSQL test namespace" {
				t.Fatalf("unsafe identifier was not refused before opening a pool: %v", err)
			}
		})
	}
}

func TestKeepSchemasRequiresExplicitOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(KeepSchemasVariable, value)
			if got := KeepSchemas(t); got != (value == "1") {
				t.Fatalf("KeepSchemas=%v for %q", got, value)
			}
		})
	}
}
