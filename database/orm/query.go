package orm

import (
	"context"
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"reflect"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Table retains the model and primary-key types. Example: Table[Order,model.ID[Order]].
type Table[M any, K comparable] struct {
	core *tableCore
	key  func() Key[M, K]
}

// TableOf declares the primary key once. Example: tbl := TableOf(b,&o.ID).
func TableOf[M any, K comparable](b Builder[M], ptr *K, opts ...ColumnOption[K]) Table[M, K] {
	k := keyOf(b, ptr, opts...)
	if b.state != nil && !b.state.sealed {
		b.state.tableBound = true
	}
	return Table[M, K]{core: k.key.core.owner, key: func() Key[M, K] { return k }}
}

func keyOf[M any, K comparable](b Builder[M], ptr *K, opts ...ColumnOption[K]) Key[M, K] {
	codec, check := keyCodec[M](opts)
	c := bindModel[M](b.state, ptr, codec, false, opts...)
	c.core.check = check
	if b.state != nil && !b.state.sealed {
		if c.core.uuid && reflect.TypeFor[K]() != reflect.TypeFor[model.ID[M]]() {
			b.state.fail(c.core.field.Name, "primary model.ID must belong to this model")
		}
		if c.core.sensitive {
			b.state.fail(c.core.field.Name, "primary key codec must not be sensitive")
		}
	}
	if b.state != nil && !b.state.sealed {
		if b.state.core.model.Primary != "" {
			b.state.fail(c.core.field.Name, "declare exactly one primary key with TableOf")
		} else {
			b.state.core.model.Primary = c.core.metadata.Name
		}
	}
	return Key[M, K]{key: c}
}
func (t *Table[M, K]) ormAttach(core *tableCore) *tableCore { old := t.core; t.core = core; return old }
func (t Table[M, K]) ormError() error                       { return tableError(t.core) }

// Key returns the already bound primary column. Example: orm.KeySet(tbl.Key(), id).
func (t Table[M, K]) Key() Key[M, K] {
	if t.key == nil {
		return Key[M, K]{}
	}
	return t.key()
}

// Query starts an immutable model query. Example: Orders.Query().Limit(20).
func (t Table[M, K]) Query() Query[M] { return Query[M]{state: newQuery(t.core)} }

// Find selects by a typed primary key. Example: orm.Find(ctx, db, Orders.Table, id).
func Find[M any, K comparable](ctx context.Context, ex database.Executor, t Table[M, K], key K) (value.Optional[M], error) {
	return First(ctx, ex, t.Query().Where(KeyEq(t.Key(), key)))
}

// RequireKey selects a key or returns database.NotFound. Example: orm.RequireKey(ctx, db, Orders.Table, id).
func RequireKey[M any, K comparable](ctx context.Context, ex database.Executor, t Table[M, K], key K) (M, error) {
	return Require(ctx, ex, t.Query().Where(KeyEq(t.Key(), key)))
}

// Query stores only a model-independent AST and table binding. Example: Orders.Query().Where(orm.KeyEq(Orders.ID, id)).
type Query[M any] struct {
	_     [0]*M
	state queryState
}

// LockedQuery requires a transaction for every terminal. Example: orm.AllLocked(ctx, tx, orm.Lock(Orders.Query())).
type LockedQuery[M any] struct {
	_                [0]*M
	lockedQueryPhase [0]struct{}
	state            queryState
}

type queryState struct {
	relations      []*relationPlan
	relationLimits *RelationLimits
	ast            querycore.QueryCore
	table          *tableCore
	lock           querycore.LockStrength
	behavior       querycore.LockBehavior
	err            error
}

func newQuery(t *tableCore) queryState {
	q := queryState{table: t}
	if t != nil {
		q.ast = querycore.QueryCore{Table: t.model.Table, Model: &t.model}
	}
	return q
}
func tableError(t *tableCore) error {
	if t == nil {
		return invalid("uninitialized table")
	}
	if t.check != nil {
		return t.check()
	}
	return t.err
}
func validateQuery(q queryState) error {
	return safeerror.Join("operation failed", tableError(q.table), q.err, q.ast.Validate(), validateRelations(q))
}

// Where appends AND predicates. Example: Orders.Query().Where(orm.NumberGt(Orders.TotalCents, 0)).
func (q Query[M]) Where(ps ...Predicate[M]) Query[M] { q.state = where(q.state, ps); return q }
func where[M any](q queryState, ps []Predicate[M]) queryState {
	q.ast.Predicates = slices.Clone(q.ast.Predicates)
	for _, p := range ps {
		if p.err != nil {
			q.err = safeerror.Join("operation failed", q.err, p.err)
		}
		if !predicateOwnerMatches(p.owner, p.rawScope, q.table) {
			q.err = safeerror.Join("operation failed", q.err, invalid("predicate belongs to a different table declaration"))
		}
		q.ast.Predicates = append(q.ast.Predicates, querycore.PredicateNode(p.expression))
	}
	return q
}

// OrderBy appends sort directions. Example: Orders.Query().OrderBy(orm.KeyAsc(Orders.ID)).
func (q Query[M]) OrderBy(os ...Order[M]) Query[M] {
	q.state = orderBy(q.state, os)
	return q
}
func orderBy[M any](q queryState, os []Order[M]) queryState {
	q.ast.Orders = slices.Clone(q.ast.Orders)
	for _, o := range os {
		q.err = safeerror.Join("operation failed", q.err, o.err)
		if o.owner == nil || o.owner != q.table {
			q.err = safeerror.Join("operation failed", q.err, invalid("order belongs to a different table declaration"))
		}
		q.ast.Orders = append(q.ast.Orders, o.core)
	}
	return q
}

// Limit bounds returned rows. Example: Orders.Query().Limit(10).
func (q Query[M]) Limit(n int) Query[M] { q.state.ast.Limit = value.Set(n); return q }

// Offset skips rows. Example: Orders.Query().Offset(10).
func (q Query[M]) Offset(n int) Query[M] { q.state.ast.Offset = n; return q }

// WithTrashed includes deleted records. Example: orm.All(ctx, db, records.Query().WithTrashed()).
func (q Query[M]) WithTrashed() Query[M] {
	q.state.ast.SoftDeleteScope = querycore.AllRecords
	return q
}

// OnlyTrashed selects deleted records. Example: orm.All(ctx, db, records.Query().OnlyTrashed()).
func (q Query[M]) OnlyTrashed() Query[M] {
	q.state.ast.SoftDeleteScope = querycore.TrashedRecords
	return q
}

// Lock requests update locks. Example: orm.AllLocked(ctx, tx, orm.Lock(Orders.Query())).
func Lock[M any](q Query[M]) LockedQuery[M] {
	q.state.lock = querycore.LockUpdate
	return LockedQuery[M]{state: q.state}
}

// ForShare requests share locks. Example: orm.AllLocked(ctx, tx, orm.ForShare(Orders.Query())).
func ForShare[M any](q Query[M]) LockedQuery[M] {
	q.state.lock = querycore.LockShare
	return LockedQuery[M]{state: q.state}
}

// All hydrates complete models, discarding partial results on failure. Example: orm.All(ctx, db, Orders.Query()).
func All[M any](ctx context.Context, ex database.Executor, q Query[M]) ([]M, error) {
	return all[M](ctx, ex, q.state)
}

// First returns an omitted Optional on no match. Example: orm.First(ctx, db, Orders.Query()).
func First[M any](ctx context.Context, ex database.Executor, q Query[M]) (value.Optional[M], error) {
	return first[M](ctx, ex, q.state)
}

// Require returns database.NotFound on no match. Example: orm.Require(ctx, db, Orders.Query()).
func Require[M any](ctx context.Context, ex database.Executor, q Query[M]) (M, error) {
	return require[M](ctx, ex, q.state)
}

// Count counts the selected window. Example: orm.Count(ctx, db, Orders.Query()).
func Count[M any](ctx context.Context, ex database.Executor, q Query[M]) (int64, error) {
	return count(ctx, ex, q.state)
}

// Exists checks the selected window. Example: orm.Exists(ctx, db, Orders.Query()).
func Exists[M any](ctx context.Context, ex database.Executor, q Query[M]) (bool, error) {
	var result bool
	err := scalarRead(ctx, ex, q.state, querycore.ReadExists, &result)
	return result, err
}

// AllLocked reads locked rows within a transaction. Example: orm.AllLocked(ctx, tx, orm.Lock(Orders.Query())).
func AllLocked[M any](ctx context.Context, tx *database.Tx, q LockedQuery[M]) ([]M, error) {
	if err := lockContext(ctx, tx); err != nil {
		return nil, err
	}
	return all[M](ctx, tx, q.state)
}

// FirstLocked reads at most one locked row. Example: orm.FirstLocked(ctx, tx, orm.ForShare(Orders.Query())).
func FirstLocked[M any](ctx context.Context, tx *database.Tx, q LockedQuery[M]) (value.Optional[M], error) {
	if err := lockContext(ctx, tx); err != nil {
		return value.Optional[M]{}, err
	}
	return first[M](ctx, tx, q.state)
}

// RequireLocked reads one locked row or fails. Example: orm.RequireLocked(ctx, tx, orm.Lock(Orders.Query())).
func RequireLocked[M any](ctx context.Context, tx *database.Tx, q LockedQuery[M]) (M, error) {
	if err := lockContext(ctx, tx); err != nil {
		return *new(M), err
	}
	return require[M](ctx, tx, q.state)
}

func firstState(q queryState) queryState {
	if n, set := q.ast.Limit.Get(); !set || n > 1 {
		q.ast.Limit = value.Set(1)
	}
	if len(q.ast.Orders) == 0 && q.table != nil {
		q.ast.Orders = []querycore.QueryOrder{{Field: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}}}
	}
	return q
}
func first[M any](ctx context.Context, ex database.Executor, q queryState) (value.Optional[M], error) {
	items, err := all[M](ctx, ex, firstState(q))
	if err != nil || len(items) == 0 {
		return value.Optional[M]{}, err
	}
	return value.Set(items[0]), nil
}
func require[M any](ctx context.Context, ex database.Executor, q queryState) (M, error) {
	v, err := first[M](ctx, ex, q)
	if err != nil {
		return *new(M), err
	}
	if item, ok := v.Get(); ok {
		return item, nil
	}
	operation := "query first"
	if q.lock != 0 {
		operation = "locked model"
	}
	return *new(M), querycore.DatabaseFailure(operation, database.NotFound)
}
func all[M any](ctx context.Context, ex database.Executor, q queryState) ([]M, error) {
	items, err := allRows[M](ctx, ex, q)
	if err != nil {
		return nil, err
	}
	if err := loadPlans(ctx, ex, unsafe.Pointer(unsafe.SliceData(items)), len(items), q.relations); err != nil {
		return nil, err
	}
	return items, nil
}
func allRows[M any](ctx context.Context, ex database.Executor, q queryState) ([]M, error) {
	items := make([]M, 0)
	batch := retrievalBatch{table: q.table}
	var item M
	var destinations []any
	err := readModels(ctx, ex, q, func(row database.Row) error {
		// Each successful row is copied into the owned result slice. Scanners
		// retain only this scratch row, which is zeroed before every decode.
		item = *new(M)
		if destinations == nil {
			if err := tableError(q.table); err != nil {
				return err
			}
			destinations = modelDestinations(q.table, unsafe.Pointer(&item))
		}
		if err := hydrateInto(q.table, row, unsafe.Pointer(&item), destinations); err != nil {
			return err
		}
		items = append(items, item)
		batch.capture(unsafe.Pointer(unsafe.SliceData(items)), len(items))
		return nil
	}, readBatches(ex, &batch))
	if err != nil {
		return nil, err
	}
	return items, nil
}
func hydrate(t *tableCore, row database.Row, base unsafe.Pointer) error {
	if err := tableError(t); err != nil {
		return err
	}
	return hydrateInto(t, row, base, modelDestinations(t, base))
}

func modelDestinations(t *tableCore, base unsafe.Pointer) []any {
	destinations := make([]any, len(t.columns))
	for i, c := range t.columns {
		destinations[i] = c.scan(base)
	}
	return destinations
}

func hydrateInto(t *tableCore, row database.Row, base unsafe.Pointer, destinations []any) error {
	if err := row.Scan(destinations...); err != nil {
		return err
	}
	var key func() (driver.Value, error)
	for _, c := range t.columns {
		if c.open == nil {
			continue
		}
		if key == nil {
			// Resolve the column at declaration time and bind it lazily once per row.
			// All-NULL encrypted fields still need neither a key nor an open scope.
			var bound driver.Value
			var bindErr error
			resolved := false
			key = func() (driver.Value, error) {
				if !resolved {
					resolved = true
					if t.primary == nil {
						bindErr = invalid("encrypted field requires a primary key")
					} else {
						bound, bindErr = t.primary.get(base)
					}
				}
				return bound, bindErr
			}
		}
		if err := c.open(row, key, base); err != nil {
			return err
		}
	}

	return nil
}

type rowSource = ormcore.RowSource

func consume(ctx context.Context, rows rowSource, scan func(database.Row) error) error {
	return ormcore.Consume(ctx, rows, scan)
}
func readModels(ctx context.Context, ex database.Executor, q queryState, scan func(database.Row) error, callbacks *ormcore.ReadCallbacks) error {
	if err := executionContext(ctx, ex); err != nil {
		return err
	}
	q.ast.ScopeContext = ctx
	s, err := compileRead(q, querycore.ReadModels)
	if err != nil {
		return err
	}
	var rows *database.Rows
	if q.lock != 0 {
		rows, err = ex.Query(s.Context(ctx), s.SQLText, s.Bindings...)
	} else {
		rows, err = database.ReadQuery(s.Context(ctx), ex, s.SQLText, s.Bindings...)
	}
	if err != nil {
		return err
	}
	return ormcore.Collect(ctx, rows, scan, callbacks)
}
func executionContext(ctx context.Context, ex database.Executor) error {
	if ctx == nil || ex == nil {
		return invalid("execution requires a context and executor")
	}
	return ctx.Err()
}

type readExecutor struct{ database.Executor }

func (e readExecutor) Query(ctx context.Context, s string, args ...any) (*database.Rows, error) {
	return database.ReadQuery(ctx, e.Executor, s, args...)
}
func scalarRead(ctx context.Context, ex database.Executor, q queryState, kind querycore.ReadKind, destination any) error {
	if err := executionContext(ctx, ex); err != nil {
		return err
	}
	q.ast.ScopeContext = ctx
	s, err := compileRead(q, kind)
	if err != nil {
		return err
	}
	return database.ScanOne(s.Context(ctx), readExecutor{ex}, s.SQLText, s.Bindings, destination)
}
func count(ctx context.Context, ex database.Executor, q queryState) (int64, error) {
	var n int64
	err := scalarRead(ctx, ex, q, querycore.ReadCount, &n)
	return n, err
}
func compileRead(q queryState, kind querycore.ReadKind) (querycore.Statement, error) {
	if err := validateQuery(q); err != nil {
		return querycore.Statement{}, err
	}
	if q.lock == 0 {
		return q.ast.CompileModel(kind, q.table.selections)
	}
	node := q.ast.ModelSelect(q.table.selections)
	node.Locks = []querycore.LockSpec{{Strength: q.lock, Behavior: q.behavior}}
	return querycore.CompileRead(node, q.ast.ScopeContext, true, nil, true)
}

// Statement is an immutable compiled statement. Example: s,err := Compile(Orders.Query()).
type Statement struct{ core *querycore.StatementState }

// SQL exposes parameterized SQL. Example: statement.SQL().
func (s Statement) SQL() string { return querycore.StatementValue(s.core).SQLText }

// Arguments returns an owned binding snapshot. Example: statement.Arguments().
func (s Statement) Arguments() []any {
	a := slices.Clone(querycore.StatementValue(s.core).Bindings)
	for i, v := range a {
		if b, ok := v.([]byte); ok {
			a[i] = slices.Clone(b)
		}
	}
	return a
}

// String omits SQL and bindings from diagnostics. Example: fmt.Sprint(statement).
func (s Statement) String() string { return "query statement (redacted)" }

// GoString omits SQL and bindings from diagnostics. Example: fmt.Sprintf("%#v",statement).
func (s Statement) GoString() string { return "query statement (redacted)" }

// Compile inspects a read without I/O. Example: Compile(Orders.Query()).
func Compile[M any](q Query[M]) (Statement, error) {
	s, err := compileRead(q.state, querycore.ReadModels)
	return Statement{querycore.RetainStatement(s)}, err
}

// CompileLocked inspects a locked read without I/O. Example: CompileLocked(orm.Lock(Orders.Query())).
func CompileLocked[M any](q LockedQuery[M]) (Statement, error) {
	s, err := compileRead(q.state, querycore.ReadModels)
	return Statement{querycore.RetainStatement(s)}, err
}

func lockContext(ctx context.Context, tx *database.Tx) error {
	if tx == nil {
		return invalid("row locking requires an active transaction")
	}
	return executionContext(ctx, tx)
}

// WithoutTrashed restores the default active-record scope.
func (q Query[M]) WithoutTrashed() Query[M] {
	q.state.ast.SoftDeleteScope = querycore.ActiveRecords
	return q
}

// ForNoKeyUpdate requests a lock compatible with key-share locks.
func ForNoKeyUpdate[M any](q Query[M]) LockedQuery[M] {
	q.state.lock = querycore.LockNoKeyUpdate
	return LockedQuery[M]{state: q.state}
}

// ForKeyShare protects selected keys against deletion and key changes.
func ForKeyShare[M any](q Query[M]) LockedQuery[M] {
	q.state.lock = querycore.LockKeyShare
	return LockedQuery[M]{state: q.state}
}

// Where adds conditions to a locked query.
func (q LockedQuery[M]) Where(ps ...Predicate[M]) LockedQuery[M] {
	q.state = where(q.state, ps)
	return q
}

// OrderBy appends locked-row ordering.
func (q LockedQuery[M]) OrderBy(os ...Order[M]) LockedQuery[M] {
	q.state = Query[M]{state: q.state}.OrderBy(os...).state
	return q
}

// Limit bounds locked rows.
func (q LockedQuery[M]) Limit(n int) LockedQuery[M] { q.state.ast.Limit = value.Set(n); return q }

// Offset skips matching rows before locking.
func (q LockedQuery[M]) Offset(n int) LockedQuery[M] { q.state.ast.Offset = n; return q }

// WithTrashed includes deleted rows.
func (q LockedQuery[M]) WithTrashed() LockedQuery[M] {
	q.state.ast.SoftDeleteScope = querycore.AllRecords
	return q
}

// OnlyTrashed selects deleted rows.
func (q LockedQuery[M]) OnlyTrashed() LockedQuery[M] {
	q.state.ast.SoftDeleteScope = querycore.TrashedRecords
	return q
}

// WithoutTrashed selects active rows.
func (q LockedQuery[M]) WithoutTrashed() LockedQuery[M] {
	q.state.ast.SoftDeleteScope = querycore.ActiveRecords
	return q
}

// NoWait fails instead of waiting for conflicting locks.
func (q LockedQuery[M]) NoWait() LockedQuery[M] { q.state.behavior = querycore.LockNoWait; return q }

// SkipLocked skips rows held by conflicting locks.
func (q LockedQuery[M]) SkipLocked() LockedQuery[M] { q.state.behavior = querycore.LockSkip; return q }

// Wait restores the default waiting behavior.
func (q LockedQuery[M]) Wait() LockedQuery[M] { q.state.behavior = querycore.LockWait; return q }

// Each streams models while rows remain open when retrieval cannot run. Otherwise
// it uses closed complete batches, dispatching Retrieved before yielding models.
// Rows yielded before a later failed batch remain yielded.
func Each[M any](ctx context.Context, ex database.Executor, q Query[M], yield func(M) error) error {
	if len(q.state.relations) > 0 {
		return EachChunked(ctx, ex, q, querycore.DefaultChunkSize, yield)
	}
	return each(ctx, ex, q.state, yield)
}

// EachLocked streams locked models in the transaction.
func EachLocked[M any](ctx context.Context, tx *database.Tx, q LockedQuery[M], yield func(M) error) error {
	if err := lockContext(ctx, tx); err != nil {
		return err
	}
	if len(q.state.relations) > 0 {
		return invalid("locked streaming cannot eager-load relations; use All")
	}
	return each(ctx, tx, q.state, yield)
}
func each[M any](ctx context.Context, ex database.Executor, q queryState, yield func(M) error) error {
	if err := executionContext(ctx, ex); err != nil {
		return err
	}
	if yield == nil {
		return invalid("model iteration requires a callback")
	}
	if mayRetrieve(q.table, ex) {
		return chunks(ctx, ex, q, querycore.DefaultChunkSize, eachBatch(ctx, yield))
	}
	return readModels(ctx, ex, q, func(row database.Row) error {
		var item M
		if err := hydrate(q.table, row, unsafe.Pointer(&item)); err != nil {
			return err
		}
		return yield(item)
	}, nil)
}
