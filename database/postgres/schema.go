package postgres

import (
	"context"
	"database/sql/driver"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Per-connection state lives in pgconn's custom data, owned by one connection.
const (
	expiresKey      = "foundry.expires"
	searchPathKey   = "foundry.search_path"
	lifetimeJitter  = 5 // at most 1/5 of MaxLifetime earlier than the pool bound
	connectionUTCTZ = "UTC"
)

// schemaPath lists the application schema and then pg_temp explicitly, so
// session temporary tables can never shadow application tables. PostgreSQL
// still searches pg_catalog implicitly first and creates unqualified objects
// in the named application schema.
func schemaPath(schema string) string { return `"` + schema + `", pg_temp` }

// connectionOptions installs one connection-establishment and checkout policy.
// Schema scope is validated once per connection. A checkout re-establishes it
// only when the server-reported search_path or TimeZone differs; servers that
// do not report search_path (before PostgreSQL 18) restore on every checkout.
// Either way a checkout discards a connection whose session created temporary
// objects, so one borrower's session tables never reach the next borrower;
// PostgreSQL does not report that change, so it costs one simple-protocol
// catalog call when the scope is unchanged.
// Connections are recycled before the pool's MaxLifetime with per-connection
// jitter so a burst of connections does not expire and reconnect together.
func connectionOptions(schema string, lifetime time.Duration) []stdlib.OptionOpenDB {
	if schema == "" && lifetime <= 0 {
		return nil
	}
	restore := func(ctx context.Context, conn *pgx.Conn) error {
		if schema == "" {
			return nil
		}
		var path *string
		var zone string
		var temporary uint32
		err := conn.QueryRow(ctx, `SELECT CASE WHEN EXISTS (
SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = $1
AND pg_catalog.has_schema_privilege(oid, 'USAGE'))
THEN pg_catalog.set_config('search_path', $2, false) ELSE NULL END,
pg_catalog.set_config('TimeZone', 'UTC', false), pg_catalog.pg_my_temp_schema()`,
			pgx.QueryExecModeExec, schema, schemaPath(schema)).Scan(&path, &zone, &temporary)
		if err != nil {
			return fault.Wrap(fault.Internal, "cannot establish PostgreSQL schema scope", err)
		}
		if path == nil || *path != schemaPath(schema) || temporary != 0 {
			return fault.New(fault.Invalid, "PostgreSQL schema scope is absent, inaccessible, or holds session temporary objects")
		}
		// Record what this server reports for the established scope. An empty
		// report means the server does not announce search_path changes.
		conn.PgConn().CustomData()[searchPathKey] = conn.PgConn().ParameterStatus("search_path")
		return nil
	}
	return []stdlib.OptionOpenDB{
		stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
			if lifetime > 0 {
				early := time.Duration(rand.Int64N(int64(lifetime/lifetimeJitter) + 1))
				conn.PgConn().CustomData()[expiresKey] = time.Now().Add(lifetime - early)
			}
			return restore(ctx, conn)
		}),
		stdlib.OptionResetSession(func(ctx context.Context, conn *pgx.Conn) error {
			// database/sql ignores other reset errors. ErrBadConn discards this
			// connection and retries acquisition on another one.
			data := conn.PgConn().CustomData()
			if expires, ok := data[expiresKey].(time.Time); ok && !time.Now().Before(expires) {
				return driver.ErrBadConn
			}
			if schema == "" {
				return nil
			}
			if reported, _ := data[searchPathKey].(string); reported != "" && conn.PgConn().ParameterStatus("search_path") == reported && conn.PgConn().ParameterStatus("TimeZone") == connectionUTCTZ {
				// The temporary namespace stays assigned for the rest of the
				// session once created, even after its objects are dropped.
				var temporary uint32
				if err := conn.QueryRow(ctx, `SELECT pg_catalog.pg_my_temp_schema()`, pgx.QueryExecModeSimpleProtocol).Scan(&temporary); err != nil || temporary != 0 {
					return driver.ErrBadConn
				}
				return nil
			}
			if err := restore(ctx, conn); err != nil {
				return driver.ErrBadConn
			}
			return nil
		}),
	}
}
