package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Set is the combined output scope, separate from both inputs. Example: expr.Scope[rows.Set[User],User].
type Set[R any] struct {
	_        [0]*R
	setScope [0]struct{}
}

// SetQuery combines complete records without model mutation methods.
// Example: rows.Union(rows.ModelRecord(a),rows.ModelRecord(b)).
type SetQuery[R any] struct {
	_         [0]*R
	recordSet [0]struct{}
	core      ormcore.RecordSource
}

func (q SetQuery[R]) recordData(*R) ormcore.RecordSource { return ormcore.SetRecord(q.core) }
func (q SetQuery[R]) rowSource(*Set[R]) ormcore.Source   { return ormcore.SetRecord(q.core).Source }

func combineRecords[R any](a, b RecordSource[R], op querycore.SetOperator) SetQuery[R] {
	return SetQuery[R]{core: ormcore.CombineRecords(recordOf(a), recordOf(b), op, reflect.TypeFor[Set[R]]())}
}

// Scope exposes only the combined record's fields. Example: expr.At(combined.Scope(),Users.ID).
func (q SetQuery[R]) Scope() expr.Scope[Set[R], R] {
	r := ormcore.SetRecord(q.core)
	return expr.FoundryScope[Set[R], R](ormcore.Scope{Reference: ormcore.Reference{Table: querycore.SetAlias, Owner: r.Record.Owner}, Err: r.Source.Err}, sqlowner.Expressions())
}

// Where filters combined rows. Example: combined.Where(expr.At(combined.Scope(),Users.ID).Ne(id)).
func (q SetQuery[R]) Where(ps ...expr.Predicate[Set[R]]) SetQuery[R] {
	q.core.Source = whereSource(q.core.Source, ps)
	return q
}

// OrderBy orders combined rows independently of input ordering. Example: combined.OrderBy(id.Asc()).
func (q SetQuery[R]) OrderBy(os ...expr.ProjectionOrder[Set[R]]) SetQuery[R] {
	q.core.Source = recordOrders(q.core.Source, os)
	return q
}

// Limit bounds the combined window. Example: combined.Limit(10).
func (q SetQuery[R]) Limit(n int) SetQuery[R] { q.core.Source.Node.Limit = value.Set(n); return q }

// Offset skips combined rows. Example: combined.Offset(10).
func (q SetQuery[R]) Offset(n int) SetQuery[R] { q.core.Source.Node.Offset = n; return q }
