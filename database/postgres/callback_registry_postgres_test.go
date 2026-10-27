package postgres_test

import (
	"context"
	"errors"
	pgx "github.com/jackc/pgx/v5"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresRewriteWithShadowedTextType(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if _, err := db.Exec(t.Context(), `CREATE DOMAIN "`+schema+`".text AS pg_catalog.text CHECK (VALUE = 'shadow-domain-value')`); err != nil {
		t.Fatal("create isolated shadowing type failed")
	}
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_catalog`); err != nil {
			return err
		}
		var result int64
		if err := database.ScanOne(t.Context(), tx, rewriteBoundarySQL, []any{rewriteBoundaryProbe{}, int64(42)}, &result); err != nil {
			return err
		}
		if result != 42 {
			return errors.New("rewritten result changed under shadowing search path")
		}
		return nil
	})
	if err != nil {
		t.Fatal("rewriter failed with explicitly reordered, shadowing search_path")
	}
}

type registeredEnumAlias string

func (v registeredEnumAlias) TextValue() (pgtype.Text, error) {
	return pgtype.Text{String: string(v), Valid: true}, nil
}

func TestPostgresRegisteredTypeAliasesRemainUsable(t *testing.T) {
	setup := pgtest.Open(t)
	schema := pgtest.Namespace(t, setup)
	typeName := schema + ".alias_enum"
	if _, err := setup.Exec(t.Context(), `CREATE TYPE "`+schema+`".alias_enum AS ENUM ('value')`); err != nil {
		t.Fatal("create isolated alias enum failed")
	}
	const aliasName = "foundry_h1_registered_alias"
	var oid uint32
	var registrations atomic.Int32
	db := pgtest.Open(t, func(config *postgres.Config) {
		config.Pool.MaxOpen = 1
		config.Pool.MaxIdle = 1
		config.RegisterTypes = func(ctx context.Context, registry *postgres.TypeRegistry) error {
			typ, err := registry.LoadType(ctx, typeName)
			if err != nil {
				return err
			}
			oid = typ.OID
			if err := registry.RegisterType(typ); err != nil {
				return err
			}
			alias := *typ
			alias.Name = aliasName
			if err := registry.RegisterType(&alias); err != nil {
				return err
			}
			if err := registry.RegisterDefaultPgType(registeredEnumAlias(""), aliasName); err != nil {
				return err
			}
			registrations.Add(1)
			return nil
		}
	})
	var native *pgx.Conn
	var value string
	if err := database.ScanOne(t.Context(), db, "SELECT $1::pg_catalog.text", []any{pgx.QueryExecModeExec, rewriteBoundaryProbe{native: &native}, registeredEnumAlias("value")}, &value); err != nil || value != "value" || native == nil {
		t.Fatal("registered default alias did not encode through native inference")
	}
	original, originalOK := native.TypeMap().TypeForName(typeName)
	alias, aliasOK := native.TypeMap().TypeForName(aliasName)
	if !originalOK || !aliasOK || original.OID != oid || alias.OID != oid || original.Name != typeName || alias.Name != aliasName {
		t.Fatal("same-OID registration discarded a type name")
	}
	for _, name := range []string{typeName, aliasName} {
		typ, ok := native.TypeMap().TypeForName(name)
		if !ok {
			t.Fatal("registered alias disappeared")
		}
		encoded, err := native.TypeMap().Encode(typ.OID, pgtype.TextFormatCode, registeredEnumAlias("value"), nil)
		if err != nil || string(encoded) != "value" {
			t.Fatal("registered alias codec changed its encoding")
		}
	}
	if registrations.Load() != 1 {
		t.Fatal("no-op rewrite repeated physical-connection registration")
	}
}

type liveCachedTextCodec struct {
	pgtype.TextCodec
	plans atomic.Int64
}

func (c *liveCachedTextCodec) PlanEncode(m *pgtype.Map, oid uint32, format int16, value any) pgtype.EncodePlan {
	c.plans.Add(1)
	return c.TextCodec.PlanEncode(m, oid, format, value)
}
func TestPostgresNoopRewriteKeepsLiveEncodingPlan(t *testing.T) {
	codec := &liveCachedTextCodec{}
	db := pgtest.Open(t, func(config *postgres.Config) {
		config.Pool.MaxOpen = 1
		config.Pool.MaxIdle = 1
		config.RegisterTypes = func(_ context.Context, registry *postgres.TypeRegistry) error {
			return registry.RegisterType(&pgtype.Type{Name: "text", OID: pgtype.TextOID, Codec: codec})
		}
	})
	var native *pgx.Conn
	var answer int64
	if err := database.ScanOne(t.Context(), db, rewriteBoundarySQL, []any{rewriteBoundaryProbe{native: &native}, int64(42)}, &answer); err != nil || answer != 42 || native == nil {
		t.Fatal("live plan warmup failed")
	}
	value := pgtype.Text{String: "cached", Valid: true}
	initial := native.TypeMap().PlanEncode(pgtype.TextOID, pgtype.BinaryFormatCode, value)
	before := codec.plans.Load()
	if initial == nil || before == 0 {
		t.Fatal("live plan probe missed registered codec")
	}
	for range 3 {
		if err := database.ScanOne(t.Context(), db, rewriteBoundarySQL, []any{rewriteBoundaryProbe{}, int64(42)}, &answer); err != nil || answer != 42 {
			t.Fatal("no-op rewrite failed during cache probe")
		}
		cached := native.TypeMap().PlanEncode(pgtype.TextOID, pgtype.BinaryFormatCode, value)
		if cached == nil || codec.plans.Load() != before {
			t.Fatal("no-op rewrite invalidated a live cached encoding plan")
		}
		encoded, err := cached.Encode(value, nil)
		if err != nil || string(encoded) != "cached" {
			t.Fatal("cached encoding plan changed its result")
		}
	}
}
