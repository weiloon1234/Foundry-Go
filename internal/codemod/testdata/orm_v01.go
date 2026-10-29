package orm

type role struct{}
type textColumn[M, V, R any] struct{}
type Text[M, V any] = textColumn[M, V, role]

func (c textColumn[M, V, R]) Eq(v V) int            { return 0 }
func (c textColumn[M, V, R]) Set(v V) int           { return 0 }
func (c textColumn[M, V, R]) In(v ...V) int         { return 0 }
func (c textColumn[M, V, R]) expressionColumn(*M) V { var v V; return v }

type InputText[M, I, V any] struct{}

func (c InputText[M, I, V]) Set(v I) int           { return 0 }
func (c InputText[M, I, V]) Eq(v V) int            { return 0 }
func (c InputText[M, I, V]) expressionColumn(*M) V { var v V; return v }

type ExpressionColumn[M, V any] interface{ expressionColumn(*M) V }

func ConflictKey[M, V any](c ExpressionColumn[M, V]) int { return 0 }
func ConflictSet[M, I, V any, C interface {
	ExpressionColumn[M, V]
	Set(I) int
}](c C, input I) int                                 { return 0 }
func Concrete[M, V any](c Text[M, V]) int            { return 0 }
func WriteGet[M, I any](m M, setter func(I) int) int { return 0 }

type Query[M any] struct{}

func (q Query[M]) Where(...int) Query[M]                   { return q }
func (q Query[M]) All(ctx, db int) int                     { return 0 }
func (q Query[M]) First(ctx, db int) int                   { return 0 }
func (q Query[M]) Require(ctx, db int) int                 { return 0 }
func (q Query[M]) Count(ctx, db int) int                   { return 0 }
func (q Query[M]) Exists(ctx, db int) int                  { return 0 }
func (q Query[M]) Each(ctx, db, yield int) int             { return 0 }
func (q Query[M]) Chunk(ctx, db, n, yield int) int         { return 0 }
func (q Query[M]) EachChunked(ctx, db, n, yield int) int   { return 0 }
func (q Query[M]) Paginate(ctx, db, request int) int       { return 0 }
func (q Query[M]) SimplePaginate(ctx, db, request int) int { return 0 }
func (q Query[M]) UpdateAll(ctx, db int, as ...int) int    { return 0 }
func (q Query[M]) DeleteAll(ctx, db int) int               { return 0 }
func (q Query[M]) ForceDeleteAll(ctx, db, soft int) int    { return 0 }
func (q Query[M]) ForUpdate() LockedQuery[M]               { return LockedQuery[M]{} }
func (q Query[M]) ForShare() LockedQuery[M]                { return LockedQuery[M]{} }
func (q Query[M]) ForNoKeyUpdate() LockedQuery[M]          { return LockedQuery[M]{} }
func (q Query[M]) ForKeyShare() LockedQuery[M]             { return LockedQuery[M]{} }

type LockedQuery[M any] struct{}

func (q LockedQuery[M]) NoWait() LockedQuery[M]      { return q }
func (q LockedQuery[M]) All(ctx, db int) int         { return 0 }
func (q LockedQuery[M]) First(ctx, db int) int       { return 0 }
func (q LockedQuery[M]) Require(ctx, db int) int     { return 0 }
func (q LockedQuery[M]) Each(ctx, db, yield int) int { return 0 }

type Table[M, K any] struct{}

func (t Table[M, K]) Query() Query[M]                              { return Query[M]{} }
func (t Table[M, K]) Key() Text[M, K]                              { return Text[M, K]{} }
func (t Table[M, K]) Find(ctx, db int, key K) int                  { return 0 }
func (t Table[M, K]) Require(ctx, db int, key K) int               { return 0 }
func (t Table[M, K]) Insert(ctx, db int, as ...int) int            { return 0 }
func (t Table[M, K]) InsertMany(ctx, db int, as []int) int         { return 0 }
func (t Table[M, K]) Restore(ctx, db int, key K) int               { return 0 }
func (t Table[M, K]) Raw(ctx, db int, sql string, args ...int) int { return 0 }
func (t Table[M, K]) Update(ctx, db int, key K, as ...int) int     { return 0 }
func (t Table[M, K]) Delete(ctx, db int, key K) int                { return 0 }
func (t Table[M, K]) ForceDelete(ctx, db, soft int, key K) int     { return 0 }
