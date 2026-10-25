package postgres

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type registryPlanCounter struct {
	pgtype.TextCodec
	plans atomic.Int64
}

func (c *registryPlanCounter) PlanEncode(m *pgtype.Map, oid uint32, format int16, value any) pgtype.EncodePlan {
	c.plans.Add(1)
	return c.TextCodec.PlanEncode(m, oid, format, value)
}

func TestPostgresUnchangedProtectionKeepsCachedEncodePlan(t *testing.T) {
	m := pgtype.NewMap()
	codec := &registryPlanCounter{}
	m.RegisterType(&pgtype.Type{Name: "text", OID: pgtype.TextOID, Codec: codec})
	protectCodecMap(m)
	value := pgtype.Text{String: "cached", Valid: true}
	if m.PlanEncode(pgtype.TextOID, pgtype.BinaryFormatCode, value) == nil || codec.plans.Load() != 1 {
		t.Fatal("encode plan probe did not exercise the registered codec")
	}
	before, err := registrySentinel(m)
	if err != nil {
		t.Fatal("sentinel could not be armed")
	}
	for range 3 {
		protectCodecMap(m)
		after, err := registrySentinel(m)
		if err != nil || after != before {
			t.Fatal("unchanged protection invalidated sentinel")
		}
		plan := m.PlanEncode(pgtype.TextOID, pgtype.BinaryFormatCode, value)
		if plan == nil || codec.plans.Load() != 1 {
			t.Fatal("unchanged protection invalidated the cached encode plan")
		}
		encoded, err := plan.Encode(value, nil)
		if err != nil || string(encoded) != "cached" {
			t.Fatal("cached plan stopped encoding the original value")
		}
	}
}

func TestPostgresRegistrySentinelDetectsNativeRegistrations(t *testing.T) {
	for name, mutate := range map[string]func(*pgtype.Map){
		"new type": func(m *pgtype.Map) {
			m.RegisterType(&pgtype.Type{Name: "external", OID: 80007, Codec: pgtype.TextCodec{}})
		},
		"same descriptor": func(m *pgtype.Map) { typ, _ := m.TypeForOID(pgtype.TextOID); m.RegisterType(typ) },
		"default mapping": func(m *pgtype.Map) { m.RegisterDefaultPgType(struct{ Value int }{}, "text") },
	} {
		t.Run(name, func(t *testing.T) {
			m := pgtype.NewMap()
			protectCodecMap(m)
			before, err := registrySentinel(m)
			if err != nil {
				t.Fatal("sentinel could not be armed")
			}
			mutate(m)
			after, err := registrySentinel(m)
			if err == nil && after == before {
				t.Fatal("native registry change escaped sentinel")
			}
		})
	}
}

func TestPostgresRegistrySentinelRejectsReplacedSyntheticCodecWithoutInvokingIt(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	codec := &registryPlanCounter{}
	m.RegisterType(&pgtype.Type{Name: protectedArgumentType, OID: 0, Codec: codec})
	_, err := registrySentinel(m)
	var diagnostic *fault.Error
	if codec.plans.Load() != 0 || !errors.As(safeerror.Reveal("test", err), &diagnostic) || !strings.Contains(diagnostic.Error(), "postgres.Config.RegisterTypes") {
		t.Fatal("sentinel invoked an unowned codec or omitted the registration hook diagnostic")
	}
}
