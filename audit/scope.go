package audit

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"log/slog"
)

// Scope is a concrete, immutable dependency for domain constructors. It borrows
// one recorder and database; callers may retain it until application shutdown.
// It does not retain a service resolver or acquire another pool.
type Scope struct {
	db       *database.DB
	schema   string
	recorder *Recorder
}

func NewScope(db *database.DB, schema string, recorder *Recorder) (*Scope, error) {
	if db == nil || recorder == nil || !sqlname.Valid(schema) {
		return nil, fault.New(fault.Invalid, "audit scope requires a database, schema and recorder")
	}
	if err := recorder.config.Validate(); err != nil {
		return nil, err
	}
	return &Scope{db: db, schema: schema, recorder: recorder}, nil
}

// Within joins the caller's business transaction through a savepoint. All audit
// work must use child; the configured schema is restored afterward. Success does
// not commit the caller's transaction, and another database's transaction fails.
func (s *Scope) Within(ctx context.Context, tx *database.Tx, fn func(*database.Tx, *Recorder) error) error {
	if s == nil || fn == nil {
		return fault.New(fault.Invalid, "audit scope requires an initialized handle and callback")
	}
	return sqlscope.InSchema(ctx, tx, s.db, s.schema, func(child *database.Tx) error { return fn(child, s.recorder) })
}
func (*Scope) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit scope")) }
func (*Scope) LogValue() slog.Value           { return slog.StringValue("audit scope") }
