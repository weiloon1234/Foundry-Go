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

func expressionColumn[V any](c *columnData[V]) ormcore.Column[V] {
	if c == nil || c.core == nil || c.core.owner == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized column")}
	}
	return ormcore.Column[V]{Reference: ormcore.Reference{Table: c.core.owner.model.Table, Owner: c.core.owner}, Field: querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, Sensitive: c.core.sensitive, Collatable: c.core.collatable, Kind: c.core.kind, Nullable: c.core.metadata.Nullable, Bind: c.bind, Clone: c.clone, Scan: c.scanValue, Err: declarationCoreError(c.core.owner)}
}

// FoundryHandle supplies comparison and projection codecs for a queryable ColumnHandle to expr/rows.
func FoundryHandle[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], seal sqlowner.MetadataSeal) (ormcore.Column[V], ormcore.Column[P]) {
	if !seal.Valid() {
		return ormcore.Column[V]{Err: invalid("invalid SQL owner capability")}, ormcore.Column[P]{Err: invalid("invalid SQL owner capability")}
	}
	if c.stored == nil || c.stored.core == nil || c.stored.core.owner == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized column")}, ormcore.Column[P]{Err: invalid("uninitialized column")}
	}
	_, isText := any(*new(K)).(TextColumnKind)
	comparison := expressionColumn(c.stored)
	comparison.Text = isText
	if !c.stored.core.metadata.Nullable {
		return comparison, *(*ormcore.Column[P])(unsafe.Pointer(&comparison))
	}
	projFn, ok := c.stored.core.nullableProjection.(func() columnData[P])
	if !ok || projFn == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized nullable column")}, ormcore.Column[P]{Err: invalid("uninitialized nullable column")}
	}
	projected := projFn()
	projection := expressionColumn(&projected)
	projection.Text = isText
	return comparison, projection
}

// FoundryComparisonHandle supplies the comparison codec for a queryable ColumnHandle to expr/rows.
func FoundryComparisonHandle[M, V, I, P any, K ColumnKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], seal sqlowner.MetadataSeal) ormcore.Column[V] {
	if !seal.Valid() {
		return ormcore.Column[V]{Err: invalid("invalid SQL owner capability")}
	}
	if c.stored == nil || c.stored.core == nil || c.stored.core.owner == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized column")}
	}
	_, isText := any(*new(K)).(TextColumnKind)
	comparison := expressionColumn(c.stored)
	comparison.Text = isText
	return comparison
}

// FoundryNullableHandle supplies comparison and nullable-projection codecs for a nullable queryable ColumnHandle.
func FoundryNullableHandle[M, V, I, P any, K NullableComparableKind, R ColumnRole](c ColumnHandle[M, V, I, P, K, R], seal sqlowner.MetadataSeal) (ormcore.Column[V], ormcore.Column[value.Nullable[V]]) {
	if !seal.Valid() {
		return ormcore.Column[V]{Err: invalid("invalid SQL owner capability")}, ormcore.Column[value.Nullable[V]]{Err: invalid("invalid SQL owner capability")}
	}
	if c.stored == nil || c.stored.core == nil || c.stored.core.owner == nil || !c.stored.core.metadata.Nullable {
		return ormcore.Column[V]{Err: invalid("uninitialized nullable column")}, ormcore.Column[value.Nullable[V]]{Err: invalid("uninitialized nullable column")}
	}
	projFn, ok := c.stored.core.nullableProjection.(func() columnData[value.Nullable[V]])
	if !ok || projFn == nil {
		return ormcore.Column[V]{Err: invalid("uninitialized nullable column")}, ormcore.Column[value.Nullable[V]]{Err: invalid("uninitialized nullable column")}
	}
	_, isText := any(*new(K)).(TextColumnKind)
	comparison := expressionColumn(c.stored)
	comparison.Text = isText
	projectedData := projFn()
	projection := expressionColumn(&projectedData)
	projection.Text = isText
	return comparison, projection
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
