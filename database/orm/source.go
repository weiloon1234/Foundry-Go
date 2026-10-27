package orm

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"unsafe"
)

// ExpressionColumn preserves a column's model and SQL value type.
// Example: rows.SelectValue(Orders.Query(),Orders.TotalCents).
type ExpressionColumn[M, V, P any] interface {
	expressionColumn(*M) (ormcore.Column[V], ormcore.Column[P])
}

func requiredExpression[V any](c columnData[V]) (ormcore.Column[V], ormcore.Column[V]) {
	v := expressionColumn(c)
	return v, v
}
func expressionColumn[V any](c columnData[V]) ormcore.Column[V] {
	if c.core == nil || c.core.owner == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized column")}
	}
	return ormcore.Column[V]{Reference: ormcore.Reference{Table: c.core.owner.model.Table, Owner: c.core.owner}, Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Sensitive: c.core.sensitive, Kind: c.core.kind, Nullable: c.core.metadata.Nullable, Bind: c.bind, Clone: c.clone, Scan: c.scanValue, Err: declarationCoreError(c.core.owner)}
}
func nullableExpression[V any](c nullableColumnData[V]) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	if c.nullable == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized nullable column")}, ormcore.Column[value.Nullable[V]]{Err: invalid("uninitialized nullable column")}
	}
	return expressionColumn(c.base), expressionColumn(c.nullable())
}

// FoundryColumn supplies comparison and projection codecs to expr/rows.
func FoundryColumn[M, V, P any](column ExpressionColumn[M, V, P], seal sqlowner.MetadataSeal) (ormcore.Column[V], ormcore.Column[P]) {
	if !seal.Valid() {
		return ormcore.Column[V]{Err: invalid("invalid SQL owner capability")}, ormcore.Column[P]{Err: invalid("invalid SQL owner capability")}
	}

	if nilInterface(column) {
		return ormcore.Column[V]{Err: invalid("missing expression column")}, ormcore.Column[P]{Err: invalid("missing expression column")}
	}
	return column.expressionColumn(nil)
}

// FoundrySource supplies sealed model AST metadata to rows.
// Example: FoundrySource(Orders.Query(),sqlowner.Metadata()).
func FoundrySource[M any](q Query[M], seal sqlowner.MetadataSeal) ormcore.Source {
	if !seal.Valid() {
		return ormcore.Source{Err: invalid("invalid SQL owner capability")}
	}

	err := safeerror.Join("operation failed", declarationCoreError(q.state.table), q.state.err, q.state.ast.Validate())
	if len(q.state.relations) > 0 {
		err = safeerror.Join("operation failed", err, invalid("projection source cannot request eager-loaded model values"))
	}
	if err != nil {
		return ormcore.Source{Err: err}
	}
	node := q.state.ast.ModelSelect(q.state.table.selections)
	return ormcore.Source{Scope: reflect.TypeFor[M](), Node: node, Owners: map[string]any{q.state.ast.Table: q.state.table}, Checks: []func() error{sourceCheck(q.state)}}
}

func sourceCheck(q queryState) func() error { return func() error { return validateQuery(q) } }

// FoundryWhere appends a sealed computed predicate in a model's original scope.
// Example: FoundryWhere(q,p,sqlowner.Queries()).
func FoundryWhere[M any](q Query[M], p ormcore.Predicate, seal sqlowner.QuerySeal) Query[M] {
	if !seal.Valid() {
		q.state.err = safeerror.Join("operation failed", q.state.err, invalid("invalid SQL owner capability"))
		return q
	}

	source := ormcore.Source{Scope: reflect.TypeFor[M](), Owners: map[string]any{q.state.ast.Table: q.state.table}}
	err := safeerror.Join("operation failed", p.Err, ormcore.CheckReferenceSet(source, p.Refs, p.ReferenceFree))
	return q.Where(Predicate[M]{owner: q.state.table, expression: querycore.RetainPredicate(p.Node), err: err})
}

// FoundryOrderBy appends sealed computed row ordering in a model's original scope.
// Example: FoundryOrderBy(q,o,sqlowner.Queries()).
func FoundryOrderBy[M any](q Query[M], o ormcore.Order, seal sqlowner.QuerySeal) Query[M] {
	if !seal.Valid() {
		q.state.err = safeerror.Join("operation failed", q.state.err, invalid("invalid SQL owner capability"))
		return q
	}

	source := ormcore.Source{Scope: reflect.TypeFor[M](), Owners: map[string]any{q.state.ast.Table: q.state.table}}
	err := safeerror.Join("operation failed", o.Err, ormcore.CheckReferenceSet(source, o.Refs, o.ReferenceFree))
	order := querycore.ModelOrder(o.Node)
	return q.OrderBy(Order[M]{owner: q.state.table, core: order, err: err})
}

// FoundryModel supplies a validated model source and its existing hydration codec.
// Example: FoundryModel(q,sqlowner.Metadata()).
func FoundryModel[M any](q Query[M], seal sqlowner.MetadataSeal) (ormcore.Source, func(database.Row) (M, error)) {
	if !seal.Valid() {
		return ormcore.Source{Err: invalid("invalid SQL owner capability")}, nil
	}
	source := FoundrySource(q, seal)
	if source.Err != nil {
		return source, nil
	}
	table := q.state.table
	return source, func(row database.Row) (M, error) {
		var model M
		if err := hydrate(table, row, unsafe.Pointer(&model)); err != nil {
			return *new(M), err
		}
		return model, nil
	}
}
