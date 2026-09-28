package postgres

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

func TestSchemaConfigurationIsTypedAndQuoted(t *testing.T) {
	for _, name := range []string{"public", "isolated_1", "MixedCase"} {
		c := explicitConfig()
		c.Schema = name
		parsed, err := connectionConfig(c)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.RuntimeParams["search_path"] != `"`+name+`"` || len(schemaOptions(name)) != 2 {
			t.Fatal("schema scope was not quoted/owned")
		}
	}
	for _, name := range []string{"a,b", "public;SELECT 1", "pg_catalog", "PG_temp", "information_schema", "a.b", "", strings.Repeat("a", 64)} {
		c := explicitConfig()
		c.Schema = name
		if name == "" {
			if err := c.Validate(); err != nil || len(schemaOptions(name)) != 0 {
				t.Fatal("unscoped compatibility lost", err)
			}
			continue
		}
		if err := c.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid schema accepted: %q", name)
		}
	}
}
