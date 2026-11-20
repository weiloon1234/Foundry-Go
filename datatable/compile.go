package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Statements are the SQL statements one Query request runs, compiled without
// executing them.
//
// Example: statements.Rows.SQL()
type Statements struct {
	// Rows reads the requested page in the requested and stable order.
	Rows orm.Statement
	// Count reads the unpaginated total, without ordering, as Query and Count do.
	Count orm.Statement
}

// Compile runs a Query request through request validation, authorization, the
// table's source, dynamic columns, filters, search and sort, and returns the
// statements Query would execute, without a database. The table's Authorize,
// Source and dynamic builders run, so their side effects happen. Compile does not
// need a Manager and does not check that the table is registered with one; the
// result matches a Manager's Query only when config equals that Manager's Config,
// which sets the page bounds and the default text collation.
//
// Example: statements, err := datatable.Compile(ctx, Members, config, authority, request)
func Compile[S, R, A any](ctx context.Context, table Table[S, R, A], config Config, subject A, request Request) (Statements, error) {
	if err := table.Validate(); err != nil {
		return Statements{}, err
	}
	if ctx == nil {
		return Statements{}, invalid("datatable compilation requires a context")
	}
	config, err := config.resolved()
	if err != nil {
		return Statements{}, err
	}
	var result Statements
	// Authorization, the source and dynamic columns run application code.
	err = callback.Isolated("datatable compile", func() error {
		prepared, err := table.definition.prepare(request, config)
		if err != nil {
			return err
		}
		source, err := table.definition.scoped(ctx, subject, QueryAction, prepared)
		if err != nil {
			return err
		}
		if result.Count, err = rows.FoundryCompileCount(ctx, source.count, sqlowner.Queries()); err != nil {
			return err
		}
		result.Rows, err = rows.FoundryCompileRead(ctx, pageOf(source.rows, prepared.page), sqlowner.Queries())
		return err
	})
	if err != nil {
		return Statements{}, err
	}
	return result, nil
}
