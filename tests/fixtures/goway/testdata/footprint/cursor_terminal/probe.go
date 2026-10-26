package cursor_terminal

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Record(ctx context.Context) (orm.Statement, error) {
	q := rows.CursorFor(rows.ModelRecord(models.Users.Query().Limit(10)))
	q = q.UniqueBy(expr.Group(expr.At(q.Scope(), models.Users.ID)))
	if err := rows.PreflightCursor(ctx, q); err != nil {
		return orm.Statement{}, err
	}
	return rows.CompileCursor(q)
}
func Scalar(ctx context.Context) (orm.Statement, error) {
	q := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Row(expr.Value(models.Users.Nickname)).Value())))
	q = q.UniqueBy(expr.Group(q.Value()))
	if err := rows.PreflightValueCursor(ctx, q); err != nil {
		return orm.Statement{}, err
	}
	return rows.CompileValueCursor(q)
}
