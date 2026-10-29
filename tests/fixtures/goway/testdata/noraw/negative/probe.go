package negative

import (
	"context"

	"foundry.test/goway/models"
	db "github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	x "github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

type wrapper struct{ db.Executor }
type localExecutor interface {
	Query(context.Context, string, ...any) (*db.Rows, error)
}

func Run(ctx context.Context, ex db.Executor, database *db.DB, tx *db.Tx, session *db.Session, primary db.PrimaryExecutor, local localExecutor, w wrapper) {
	_, _ = ex.Exec(ctx, "private_sql_canary", "private_argument_canary") // want database.Exec
	_, _ = ex.Query(ctx, "private_sql_canary")                           // want database.Query
	_, _ = database.QueryRead(ctx, "private_sql_canary")                 // want database.QueryRead
	_, _ = tx.Query(ctx, "private_sql_canary")                           // want database.Query
	_, _ = session.Exec(ctx, "private_sql_canary")                       // want database.Exec
	_, _ = primary.Exec(ctx, "private_sql_canary")                       // want database.Exec
	_, _ = local.Query(ctx, "private_sql_canary")                        // want database.Query
	_, _ = w.Query(ctx, "private_sql_canary")                            // want database.Query
	call := ex.Exec
	_, _ = call(ctx, "private_sql_canary") // want database.Exec
	method := db.Executor.Query
	_, _ = method(ex, ctx, "private_sql_canary")                                                                                         // want database.Query
	_ = db.ScanOne(ctx, ex, "private_sql_canary", nil)                                                                                   // want database.ScanOne
	_, _ = db.ReadQuery(ctx, ex, "private_sql_canary")                                                                                   // want database.ReadQuery
	_ = db.ForEach[int](ctx, ex, "private_sql_canary", nil, func(db.Row) (int, error) { return 0, nil }, func(int) error { return nil }) // want database.ForEach
	_, _ = orm.Raw(ctx, ex, models.Users.Table, "private_sql_canary")                                                                    // want orm.Raw
	tableRaw := orm.Raw[models.User, model.ID[models.User]]
	_, _ = tableRaw(ctx, ex, models.Users.Table, "private_sql_canary") // want orm.Raw

	var schema rows.Row[models.User]
	_, _ = rows.Raw(ctx, ex, schema, "private_sql_canary")      // want rows.Raw
	_ = x.Raw[models.User, int64]("1")                          // want expr.Raw
	_ = x.RawWithCodec[models.User](codec.Signed[int64](), "1") // want expr.RawWithCodec
	_ = orm.WhereRaw[models.User]("true")                       // want orm.WhereRaw
}
func Generic[E db.Executor](ctx context.Context, e E) {
	_, _ = e.Query(ctx, "private_sql_canary") // want database.Query
}

type rawTable interface {
	Raw(context.Context, db.Executor, orm.Table[models.User, model.ID[models.User]], string, ...any) ([]models.User, error)
}

func TableInterface(ctx context.Context, ex db.Executor, table rawTable) {
	_, _ = table.Raw(ctx, ex, models.Users.Table, "private_sql_canary") // want orm.Raw
	call := table.Raw
	_, _ = call(ctx, ex, models.Users.Table, "private_sql_canary") // want orm.Raw
}

var scanFunction = db.ScanOne
var aliasFunction = scanFunction

func FunctionVariables(ctx context.Context, ex db.Executor) {
	_ = aliasFunction(ctx, ex, "private_sql_canary", nil) // want database.ScanOne
	run := db.ForEach[int]
	_ = run(ctx, ex, "private_sql_canary", nil, func(db.Row) (int, error) { return 0, nil }, func(int) error { return nil }) // want database.ForEach
	var query func(context.Context, string, ...any) (*db.Rows, error)
	query = ex.Query
	_, _ = query(ctx, "private_sql_canary") // want database.Query
}
