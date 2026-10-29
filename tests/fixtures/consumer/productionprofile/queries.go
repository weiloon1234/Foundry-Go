package productionprofile

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// Check exercises typed assignments and query composition without acquiring resources.
func Check() error {
	if err := orm.Check(Tables()...); err != nil {
		return err
	}
	f := Records
	base := f.Query().Where(orm.ColumnEq(f.Active, true), orm.NumberGte(f.Score, 10)).OrderBy(orm.NumberDesc(f.Score), orm.KeyAsc(f.ID)).Limit(25)
	if _, err := orm.Compile(base); err != nil {
		return err
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query().Where(orm.ColumnEq(f.Active, true))), expr.Sum(orm.NumberExpr(f.Score)).Value()))); err != nil {
		return err
	}
	_ = []orm.Assignment[Record]{orm.TextSet(f.Name, "profile"), orm.ColumnSet(f.Payload, []byte("owned"))}
	return nil
}

type resourceLeft struct{}
type resourceRight struct{}

// CheckJoinedFields exercises nullable text, binary and ordered values in a self-join.
func CheckJoinedFields() error {
	left := rows.As[resourceLeft](Records.Query(), "left_records")
	right := rows.As[resourceRight](Records.Query(), "right_records")
	joined := rows.LeftJoin(left, right, rows.On(expr.At(rows.Scope(left), orm.KeyExpr(Records.ID)), expr.At(rows.Scope(right), orm.KeyExpr(Records.ID))))
	fields := rows.LeftScope(joined, rows.Scope(left))
	nullable := rows.NullableRightScope(joined, rows.Scope(right))
	caption := expr.Coalesce(expr.AtNullable(nullable, orm.TextExpr(Records.Name)), expr.At(fields, orm.TextExpr(Records.Name)))
	text := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(joined, expr.Lower(caption).Value()),
		expr.At(fields, orm.ColumnExpr(Records.Active)).Eq(true), expr.AtNullable(nullable, orm.NumberExpr(Records.Score)).IsNotNull())).OrderBy(caption.Asc(), expr.At(fields, orm.KeyExpr(Records.ID)).Asc())
	if _, err := rows.CompileRead(text); err != nil {
		return err
	}
	_, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(joined, expr.Row(expr.Coalesce(expr.AtNullable(nullable, orm.ColumnExpr(Records.Payload)), expr.At(fields, orm.ColumnExpr(Records.Payload)))).Value())))
	return err
}
