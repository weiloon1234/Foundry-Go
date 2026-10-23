package rows

import (
	"context"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionRecordSource carries complete results and nested locks in one getter.
// Example: rows.AsTransaction[tag](rows.LockedRecord(q.ForUpdate()),"claim").
type TransactionRecordSource[R any] interface{ transactionRecord(*R) ormcore.RecordSource }

// TransactionSelectionSource exposes only transaction-required input scopes.
// Example: rows.SelectTransaction(source,Summary,mappings...).
type TransactionSelectionSource[S any] interface{ transactionSelection(*S) ormcore.RecordSource }

// TransactionRecord explicitly retains the transaction requirement, without execution.
// Example: rows.TransactionOf(rows.ModelRecord(Users.Query())).
type TransactionRecord[R any] struct {
	_                      [0]*R
	transactionRecordPhase [0]struct{}
	core                   ormcore.RecordSource
}

func (r TransactionRecord[R]) transactionRecord(*R) ormcore.RecordSource { return r.core }

// TransactionOf lifts an ordinary complete record into transaction composition.
// Example: rows.TransactionOf(rows.RecordOf(report)).
func TransactionOf[R any](source RecordSource[R]) TransactionRecord[R] {
	r := recordOf(source)
	r.Transaction = true
	return TransactionRecord[R]{core: r}
}

// LockedRecord retains all model locks and their wait policy inside the source.
// Example: rows.LockedRecord(Users.Query().ForUpdate().SkipLocked()).
func LockedRecord[M any](q orm.LockedQuery[M]) TransactionRecord[M] {
	return TransactionRecord[M]{core: orm.FoundryLockedRecord(q, sqlowner.Metadata())}
}

// TransactionAlias gives transaction-owned fields a distinct scope from ordinary aliases.
// Example: TransactionAlias[claimed,Order].
type TransactionAlias[A, R any] struct {
	_                     [0]*A
	_                     [0]*R
	transactionAliasPhase [0]struct{}
}

// TransactionAliased names one complete record without ordinary source methods.
// Example: rows.AsTransaction[claimed](record,"claimed").
type TransactionAliased[A, R any] struct {
	_    [0]*A
	_    [0]*R
	core ormcore.RecordSource
}

func (a TransactionAliased[A, R]) transactionSelection(*TransactionAlias[A, R]) ormcore.RecordSource {
	return a.core
}
func transactionRecordOf[R any](source TransactionRecordSource[R]) ormcore.RecordSource {
	if ormcore.NilDescriptor(source) {
		return ormcore.RecordSource{Source: ormcore.Source{Err: ormcore.Invalid("transaction record requires a source")}}
	}
	r := source.transactionRecord(nil)
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[R](), true)
	return r
}
func transactionSelectionOf[S any](source TransactionSelectionSource[S]) ormcore.Source {
	if ormcore.NilDescriptor(source) {
		return ormcore.Source{Err: ormcore.Invalid("transaction selection requires a source")}
	}
	r := source.transactionSelection(nil)
	if !r.Transaction || r.Source.Scope != reflect.TypeFor[S]() {
		r.Source.Err = ormcore.Invalid("transaction selection requires its declared scope")
	}
	return r.Source
}

// AsTransaction retains locks within the aliased SELECT and its executor kind.
// Example: rows.AsTransaction[claimed](rows.LockedRecord(q.ForUpdate()),"claimed").
func AsTransaction[A, R any](source TransactionRecordSource[R], alias string) TransactionAliased[A, R] {
	r := transactionRecordOf(source)
	r.Source = ormcore.AliasRecord(r, alias, reflect.TypeFor[R](), reflect.TypeFor[TransactionAlias[A, R]](), true)
	r.Transaction = true
	r.Direct = false
	return TransactionAliased[A, R]{core: r}
}

// TransactionScope exposes preserved transaction-owned model or projection fields.
// Example: expr.At(rows.TransactionScope(source),Orders.ID).
func TransactionScope[A, R any](source TransactionAliased[A, R]) expr.Scope[TransactionAlias[A, R], R] {
	s := source.core.Source
	return expr.FoundryScope[TransactionAlias[A, R], R](ormcore.Scope{Reference: ormcore.Reference{Table: s.Node.Source.Name(), Owner: s.Owners[s.Node.Source.Name()]}, Err: s.Err}, sqlowner.Expressions())
}

// TransactionRead returns a complete result only through a transaction executor.
// Example: selected.All(ctx,tx).
type TransactionRead[S, R any] struct {
	_                    [0]*S
	_                    [0]*R
	transactionReadPhase [0]struct{}
	core                 ormcore.RecordSource
}

func (q TransactionRead[S, R]) transactionRecord(*R) ormcore.RecordSource { return q.core }

// SelectTransaction reuses complete projection mapping and declaration validation.
// Example: rows.SelectTransaction(source,Summary,rows.Map(Summary.ID,id)).
func SelectTransaction[S, R any](source TransactionSelectionSource[S], schema Schema[R], mappings ...Mapping[S, R]) TransactionRead[S, R] {
	q := Select(selectionAdapter[S]{source: transactionSelectionOf(source)}, schema, mappings...)
	r := RecordOf(q).core
	r.Transaction = true
	return TransactionRead[S, R]{core: r}
}

// SelectTransactionRecord selects every field from a preserved transaction scope.
// Example: rows.SelectTransactionRecord(source,rows.TransactionScope(source)).
func SelectTransactionRecord[S, R any](source TransactionSelectionSource[S], scope expr.Scope[S, R]) TransactionRead[S, R] {
	s := expr.FoundryScopeData(scope, sqlowner.Expressions())
	r := ormcore.SelectRecord(transactionSelectionOf(source), s, recordDefinition[R](s.Owner), reflect.TypeFor[R]())
	r.Transaction = true
	return TransactionRead[S, R]{core: r}
}

// Where retains transaction input ownership. Example: selected.Where(id.Gt(0)).
func (q TransactionRead[S, R]) Where(ps ...expr.Predicate[S]) TransactionRead[S, R] {
	q.core.Source = whereSource(q.core.Source, ps)
	q.core.Direct = false
	return q
}

// OrderBy orders the transaction selection. Example: selected.OrderBy(id.Asc()).
func (q TransactionRead[S, R]) OrderBy(os ...expr.ProjectionOrder[S]) TransactionRead[S, R] {
	q.core.Source = recordOrders(q.core.Source, os)
	q.core.Direct = false
	return q
}

// Limit retains the transaction selection's window. Example: selected.Limit(10).
func (q TransactionRead[S, R]) Limit(n int) TransactionRead[S, R] {
	q.core.Source.Node.Limit = value.Set(n)
	q.core.Direct = false
	return q
}

// Offset retains the transaction selection's offset. Example: selected.Offset(10).
func (q TransactionRead[S, R]) Offset(n int) TransactionRead[S, R] {
	q.core.Source.Node.Offset = n
	q.core.Direct = false
	return q
}

// All requires the caller's transaction and discards every partial result on error.
// Example: selected.All(ctx,tx).
func (q TransactionRead[S, R]) All(ctx context.Context, tx *database.Tx) ([]R, error) {
	return readRecord[R](ctx, tx, q.core, true)
}
