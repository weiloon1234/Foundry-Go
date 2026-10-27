package postgres

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

func TestTypeRegistryPlainDiagnosticsSurviveSQLContainment(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{"nil registry", "PostgreSQL type registry is unavailable"},
		{"zero registry", "PostgreSQL type registry is unavailable"},
		{"closed registry", "PostgreSQL type registry is closed"},
		{"nil descriptor", "register PostgreSQL type: invalid PostgreSQL type registration"},
		{"missing codec", "register PostgreSQL type: invalid PostgreSQL type registration"},
		{"zero identity", "register PostgreSQL type: invalid PostgreSQL type registration"},
		{"reserved descriptor", "register PostgreSQL type: invalid PostgreSQL type registration"},
		{"reserved mapping", "register PostgreSQL Go type: reserved PostgreSQL type mapping"},
		{"missing connection", "load PostgreSQL type: PostgreSQL type loading requires a connection"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, registry := testTypeRegistry()
			before, err := registrySentinel(m)
			if err != nil {
				t.Fatal("registry fixture has no protection sentinel")
			}
			switch test.name {
			case "nil registry":
				registry = nil
			case "zero registry":
				registry = &TypeRegistry{}
			case "closed registry":
				registry.closed = true
			}
			var original error
			sealed := sqlcallback.Invoke("initialize PostgreSQL types", func() error {
				switch test.name {
				case "missing codec":
					original = registry.RegisterType(&pgtype.Type{Name: "missing_codec", OID: 81020})
				case "zero identity":
					original = registry.RegisterType(&pgtype.Type{Name: "zero_identity", Codec: pgtype.TextCodec{}})
				case "reserved descriptor":
					original = registry.RegisterType(&pgtype.Type{Name: protectedArgumentType, OID: 81020, Codec: pgtype.TextCodec{}})
				case "reserved mapping":
					original = registry.RegisterDefaultPgType(registryValue{}, protectedArgumentType)
				case "missing connection":
					_, original = registry.LoadType(t.Context(), "text")
				default:
					original = registry.RegisterType(nil)
				}
				return original
			})
			native := fmt.Errorf("native connection setup: %w", sealed)
			err = callback.Isolated("construct database", func() error {
				return sqlcallback.Retain(native, safeerror.Reveal("connection setup failed", native))
			})
			want := "construct database: initialize PostgreSQL types: " + test.message
			var classified *fault.Error
			if original == nil || err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, native) || errors.As(err, &classified) {
				t.Fatal("registration diagnostic lost text, identity or classification neutrality")
			}
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want {
				t.Fatal("registration diagnostic formatting changed")
			}
			if registry != nil && !registry.closed && registry.RegisterDefaultPgType(registryValue{}, "int8") != original {
				t.Fatal("failed registration did not retain the original failure")
			}
			after, sentinelErr := registrySentinel(m)
			if sentinelErr != nil || after != before {
				t.Fatal("rejected registration invalidated the protection sentinel")
			}
		})
	}
}

func TestPostgresCodecPlainDiagnosticsSurviveNativeWrapping(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{"unsupported argument", "encode PostgreSQL argument: unsupported PostgreSQL argument"},
		{"invalid driver value", "encode PostgreSQL argument: convert PostgreSQL argument: invalid SQL driver value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := pgtype.NewMap()
			protectCodecMap(m)
			var formatting atomic.Int32
			var input any = make(chan int)
			if test.name == "invalid driver value" {
				input = invalidNativeValuer{calls: &formatting}
			}
			data, original := m.Encode(pgtype.Int8OID, pgtype.BinaryFormatCode, protectValue(m, input), nil)
			err := callback.Isolated("bind PostgreSQL value", func() error {
				return sqlcallback.Retain(original, safeerror.Reveal("PostgreSQL encoding failed", original))
			})
			want := "bind PostgreSQL value: " + test.message
			var classified *fault.Error
			if data != nil || original == nil || err == nil || err.Error() != want || !errors.Is(err, original) || errors.As(err, &classified) {
				t.Fatal("native PostgreSQL encoding lost its neutral source diagnostic")
			}
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || formatting.Load() != 0 {
				t.Fatal("invalid PostgreSQL argument was formatted")
			}
		})
	}
}

func TestPostgresValuerApplicationErrorsRemainPrivate(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &nativePrivateError{mode: mode}
			var valuer driver.Valuer = checkedValuer{source: failingSQLValuer{cause: cause}}
			_, original := driver.DefaultParameterConverter.ConvertValue(valuer)
			err := safeerror.Reveal("PostgreSQL argument failed", original)
			var retained *nativePrivateError
			var classified *fault.Error
			if err == nil || err.Error() != "PostgreSQL argument failed" || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause || errors.As(err, &classified) {
				t.Fatal("PostgreSQL valuer changed application error identity or classification")
			}
			if fmt.Sprintf("%v %+v %#v", err, err, err) != "PostgreSQL argument failed PostgreSQL argument failed PostgreSQL argument failed" || cause.calls.Load() != 0 {
				t.Fatal("PostgreSQL valuer formatted an application error")
			}
		})
	}
}
