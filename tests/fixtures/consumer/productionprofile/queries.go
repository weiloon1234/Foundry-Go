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
	base := f.Query().Where(f.Active.Eq(true), f.Score.Gte(10)).OrderBy(f.Score.Desc(), f.ID.Asc()).Limit(25)
	if _, err := orm.Compile(base); err != nil {
		return err
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query().Where(f.Active.Eq(true))), expr.Sum(f.Score).Value()))); err != nil {
		return err
	}
	_ = []orm.Assignment[Record]{f.Name.Set("profile"), f.Payload.Set([]byte("owned"))}
	return nil
}

type resourceLeft struct{}
type resourceRight struct{}

// CheckJoinedFields exercises nullable text, binary and ordered values in a self-join.
func CheckJoinedFields() error {
	left := rows.As[resourceLeft](Records.Query(), "left_records")
	right := rows.As[resourceRight](Records.Query(), "right_records")
	joined := rows.LeftJoin(left, right, rows.On(expr.At(rows.Scope(left), Records.ID), expr.At(rows.Scope(right), Records.ID)))
	fields := rows.LeftScope(joined, rows.Scope(left))
	nullable := rows.NullableRightScope(joined, rows.Scope(right))
	caption := expr.Coalesce(expr.AtNullable(nullable, Records.Name), expr.At(fields, Records.Name))
	text := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(joined, expr.Lower(caption).Value()),
		expr.At(fields, Records.Active).Eq(true), expr.AtNullable(nullable, Records.Score).IsNotNull())).OrderBy(caption.Asc(), expr.At(fields, Records.ID).Asc())
	if _, err := rows.CompileRead(text); err != nil {
		return err
	}
	_, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(joined, expr.Row(expr.Coalesce(expr.AtNullable(nullable, Records.Payload), expr.At(fields, Records.Payload))).Value())))
	return err
}
