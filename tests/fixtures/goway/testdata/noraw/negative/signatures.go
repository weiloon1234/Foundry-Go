package negative

import (
	"context"

	"foundry.test/goway/models"
	db "github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
)

type queryValue func(context.Context, string, ...any) (*db.Rows, error)

// Matching depends on signatures, even without any binding to a raw symbol.
func ValueExpressions(ctx context.Context, ex db.Executor, f queryValue, field struct{ Run queryValue }) {
	_, _ = (*(&f))(ctx, "private_sql_canary")            // want database.Query
	_, _ = field.Run(ctx, "private_sql_canary")          // want database.Query
	_, _ = []queryValue{f}[0](ctx, "private_sql_canary") // want database.Query
	_, _ = identity(f)(ctx, "private_sql_canary")        // want database.Query
	_, _ = queryValue(f)(ctx, "private_sql_canary")      // want database.Query
	p := &f
	*p = ex.Query
	_, _ = (*(&f))(ctx, "private_sql_canary") // want database.Query
}
func identity(f queryValue) queryValue { return f }

func noQuery(context.Context, string, ...any) (*db.Rows, error) { return nil, nil }

func Reassignment(ctx context.Context, ex db.Executor) {
	// Direct application functions are classified by identity, not signature.
	_, _ = noQuery(ctx, "safe")
	f := noQuery
	_, _ = f(ctx, "safe") // want database.Query
	f = ex.Query
	_, _ = f(ctx, "private_sql_canary") // want database.Query
	f = noQuery
	_, _ = f(ctx, "safe") // want database.Query
	// Nonmatching signatures remain clean before and after reassignment.
	safe := func() {}
	safe()
	safe = func() { _, _ = ex.Query(ctx, "private_sql_canary") } // want database.Query
	safe()
}

func GenericSignatures(ctx context.Context, ex db.Executor,
	table func(context.Context, db.Executor, orm.Table[models.User, model.ID[models.User]], string, ...any) ([]models.User, error),
	row func(context.Context, db.Executor, rows.Schema[models.User], string, ...any) ([]models.User, error),
	raw func(string, ...any) expr.Selected[models.User, int64],
	coded func(codec.Codec[int64], string, ...any) expr.Selected[models.User, int64],
	predicate func(string, ...any) orm.Predicate[models.User],
	each func(context.Context, db.Executor, string, []any, func(db.Row) (int, error), func(int) error) error,
) {
	_, _ = table(ctx, ex, models.Users.Table, "private_sql_canary")                                                           // want orm.Raw
	_, _ = row(ctx, ex, rows.Row[models.User]{}, "private_sql_canary")                                                        // want rows.Raw
	_ = raw("private_sql_canary")                                                                                             // want expr.Raw
	_ = coded(codec.Signed[int64](), "private_sql_canary")                                                                    // want expr.RawWithCodec
	_ = predicate("private_sql_canary")                                                                                       // want orm.WhereRaw
	_ = each(ctx, ex, "private_sql_canary", nil, func(db.Row) (int, error) { return 0, nil }, func(int) error { return nil }) // want database.ForEach
	read := db.ReadQuery
	_, _ = read(ctx, ex, "private_sql_canary") // want database.ReadQuery
}

type genericRawTable[R any, K comparable] interface {
	Raw(context.Context, db.Executor, orm.Table[R, K], string, ...any) ([]R, error)
}

func TableContracts[R any, K comparable, T genericRawTable[R, K]](ctx context.Context, ex db.Executor, table T, r genericRawTable[R, K], definition orm.Table[R, K]) {
	_, _ = table.Raw(ctx, ex, definition, "private_sql_canary") // want orm.Raw
	_, _ = r.Raw(ctx, ex, definition, "private_sql_canary")     // want orm.Raw
	call := table.Raw
	_, _ = call(ctx, ex, definition, "private_sql_canary") // want orm.Raw
}

func Invoke[F ~func(context.Context, string, ...any) (*db.Rows, error)](ctx context.Context, f F) {
	_, _ = f(ctx, "private_sql_canary")       // want database.Query
	_, _ = (*(&f))(ctx, "private_sql_canary") // want database.Query
}

type queryCallable interface {
	~func(context.Context, string, ...any) (*db.Rows, error)
}
type embeddedCallable interface{ queryCallable }

func InvokeEmbedded[F embeddedCallable](ctx context.Context, f F) {
	_, _ = f(ctx, "private_sql_canary") // want database.Query
}

type otherQueryValue queryValue

func InvokeUnion[F queryValue | otherQueryValue](ctx context.Context, f F) {
	_, _ = f(ctx, "private_sql_canary") // want database.Query
}
func InvokeExact[F func(context.Context, string, ...any) (*db.Rows, error)](ctx context.Context, f F) {
	_, _ = f(ctx, "private_sql_canary") // want database.Query
}
func InvokeRaw[S, V any, F ~func(string, ...any) expr.Selected[S, V]](f F) {
	_ = f("private_sql_canary") // want expr.Raw
}

func InvokeORMRaw[M any, K comparable, F ~func(context.Context, db.Executor, orm.Table[M, K], string, ...any) ([]M, error)](ctx context.Context, ex db.Executor, table orm.Table[M, K], f F) {
	_, _ = f(ctx, ex, table, "private_sql_canary")       // want orm.Raw
	_, _ = (*(&f))(ctx, ex, table, "private_sql_canary") // want orm.Raw
}
