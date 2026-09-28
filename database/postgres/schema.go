package postgres

import (
	"context"
	"database/sql/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Only the named schema is explicit. PostgreSQL searches pg_catalog implicitly
// first, but creates unqualified objects in the named application schema.
func schemaOptions(schema string) []stdlib.OptionOpenDB {
	if schema == "" {
		return nil
	}
	restore := func(ctx context.Context, conn *pgx.Conn) error {
		var path *string
		var zone string
		var temporary uint32
		err := conn.QueryRow(ctx, `SELECT CASE WHEN EXISTS (
SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = $1
AND pg_catalog.has_schema_privilege(oid, 'USAGE'))
THEN pg_catalog.set_config('search_path', $2, false) ELSE NULL END,
pg_catalog.set_config('TimeZone', 'UTC', false), pg_catalog.pg_my_temp_schema()`,
			pgx.QueryExecModeExec, schema, `"`+schema+`"`).Scan(&path, &zone, &temporary)
		if err != nil {
			return fault.Wrap(fault.Internal, "cannot establish PostgreSQL schema scope", err)
		}
		if path == nil || *path != `"`+schema+`"` || temporary != 0 {
			return fault.New(fault.Invalid, "PostgreSQL schema scope is absent, inaccessible, or shadowed by session tables")
		}
		return nil
	}
	return []stdlib.OptionOpenDB{
		stdlib.OptionAfterConnect(restore),
		stdlib.OptionResetSession(func(ctx context.Context, conn *pgx.Conn) error {
			// database/sql ignores other reset errors. Never lend a failed scope.
			if err := restore(ctx, conn); err != nil {
				return driver.ErrBadConn
			}
			return nil
		}),
	}
}
