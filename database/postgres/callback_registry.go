package postgres

import (
	"context"
	pgx "github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

const registrationRequired = "PostgreSQL type registration requires postgres.Config.RegisterTypes"

// pgx invalidates cached encode plans whenever its registration methods run.
// This private value uses only the framework-owned synthetic codec. The nonzero
// plan allocation makes a changed cache distinguishable by pointer identity.
// It never performs encoding or sends a server statement.
type registrationProbe struct{}
type registrationPlan struct{ marker byte }

func (*registrationPlan) Encode(any, []byte) ([]byte, error) {
	return nil, registrationFailure()
}

func registrationFailure() error {
	return safeerror.Seal(registrationRequired, safeerror.NewFault(fault.Invalid, registrationRequired))
}

func registrySentinel(m *pgtype.Map) (*registrationPlan, error) {
	if m == nil {
		return nil, registrationFailure()
	}
	typ, ok := m.TypeForOID(0)
	if !ok || typ == nil || typ.Name != protectedArgumentType {
		return nil, registrationFailure()
	}
	codec, ok := typ.Codec.(protectedCodec)
	named, _ := m.TypeForName(protectedArgumentType)
	if !ok || codec.native != nil || codec.registry == nil || named != typ {
		return nil, registrationFailure()
	}
	plan, ok := m.PlanEncode(0, pgtype.TextFormatCode, registrationProbe{}).(*registrationPlan)
	if !ok || plan == nil {
		return nil, registrationFailure()
	}
	return plan, nil
}

// A failed callback or registry check may leave unprotected registrations. Do
// not return that connection to the pool or expose a native retry control that
// could replay application code.
func discardRewriterConnection(ctx context.Context, conn *pgx.Conn) {
	_ = sqlcallback.Invoke("discard PostgreSQL callback connection", func() error { return conn.Close(ctx) })
}
