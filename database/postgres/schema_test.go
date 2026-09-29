package postgres

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
	"time"
)

func TestSessionTimeoutsBecomeWholeMillisecondRuntimeParameters(t *testing.T) {
	c := explicitConfig()
	c.StatementTimeout, c.LockTimeout, c.IdleInTransactionSessionTimeout = 1500*time.Millisecond, 2*time.Second, time.Minute
	parsed, err := connectionConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.RuntimeParams["statement_timeout"] != "1500" || parsed.RuntimeParams["lock_timeout"] != "2000" || parsed.RuntimeParams["idle_in_transaction_session_timeout"] != "60000" {
		t.Fatal("session timeouts were not applied as runtime parameters", parsed.RuntimeParams)
	}
	unset, err := connectionConfig(explicitConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, set := unset.RuntimeParams["statement_timeout"]; set {
		t.Fatal("zero timeout replaced the server default")
	}
	for _, invalid := range []time.Duration{-time.Millisecond, time.Microsecond, time.Duration(1<<31) * time.Millisecond} {
		c := explicitConfig()
		c.LockTimeout = invalid
		if err := c.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid session timeout accepted", invalid)
		}
	}
}

func TestSchemaConfigurationIsTypedAndQuoted(t *testing.T) {
	for _, name := range []string{"public", "isolated_1", "MixedCase"} {
		c := explicitConfig()
		c.Schema = name
		parsed, err := connectionConfig(c)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.RuntimeParams["search_path"] != `"`+name+`", pg_temp` || len(connectionOptions(name, 0)) != 2 {
			t.Fatal("schema scope was not quoted/owned")
		}
	}
	for _, name := range []string{"a,b", "public;SELECT 1", "pg_catalog", "PG_temp", "information_schema", "a.b", "", strings.Repeat("a", 64)} {
		c := explicitConfig()
		c.Schema = name
		if name == "" {
			if err := c.Validate(); err != nil || len(connectionOptions(name, 0)) != 0 || len(connectionOptions(name, time.Minute)) != 2 {
				t.Fatal("unscoped compatibility lost", err)
			}
			continue
		}
		if err := c.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid schema accepted: %q", name)
		}
	}
}
