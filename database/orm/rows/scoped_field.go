package rows

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// At reads a declared projection field within its named record scope. Example: rows.At(rows.LateralScope(totals),Totals.Count).
func At[S, R, V any](scope expr.Scope[S, R], field Field[R, V]) expr.RowExpression[S, V] {
	metadata := expr.FoundryScopeData(scope, sqlowner.Expressions())
	var column ormcore.Column[V]
	err := metadata.Err
	if field.core == nil || field.column == nil {
		err = errors.Join(err, ormcore.Invalid("record field requires a declaration"))
	} else {
		column = field.column()
		if metadata.Owner != field.core.owner || metadata.Table == "" {
			err = errors.Join(err, ormcore.Invalid("record field belongs to a different declaration"))
		}
		column.Field = querycore.FieldRef{Table: metadata.Table, Column: field.core.column.Name}
	}
	column.Reference = metadata.Reference
	return expr.FoundryRowExpression[S](ormcore.Expression{Node: column.Field, Refs: []ormcore.Reference{column.Reference}, Err: errors.Join(err, column.Err)}, column, sqlowner.Expressions())
}

// AtNullable widens a required projection field from a nullable join side. Example: rows.AtNullable(rows.NullableRightScope(j,scope),Totals.Count).
func AtNullable[S, R, V any](scope expr.NullableScope[S, R], field Field[R, V]) expr.RowExpression[S, value.Nullable[V]] {
	s := expr.FoundryScope[S, R](expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), sqlowner.Expressions())
	return expr.NullableRow(At(s, field))
}

// AtNullableValue retains one nullable layer for a nullable projection field. Example: rows.AtNullableValue(scope,Summary.Note).
func AtNullableValue[S, R, V any](scope expr.NullableScope[S, R], field Field[R, value.Nullable[V]]) expr.RowExpression[S, value.Nullable[V]] {
	s := expr.FoundryScope[S, R](expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), sqlowner.Expressions())
	return At(s, field)
}
