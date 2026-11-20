package productionprofile

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// Check exercises typed assignments and query composition without acquiring resources.
func Check() error {
	// doc-example:begin check-table-registry
	if err := orm.Check(Tables()...); err != nil {
		return err
	}
	// doc-example:end check-table-registry
	f := Records
	base := f.Query().Where(orm.Eq(f.Active, true), orm.Gte(f.Score, 10)).OrderBy(orm.Desc(f.Score), orm.Asc(f.ID)).Limit(25)
	if _, err := orm.Compile(base); err != nil {
		return err
	}
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(f.Query().Where(orm.Eq(f.Active, true))), expr.AsSelected(expr.Sum(f.Score))))); err != nil {
		return err
	}
	_ = []orm.Assignment[Record]{orm.Set(f.Name, "profile"), orm.Set(f.Payload, []byte("owned"))}
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
	text := rows.ReadScalar(rows.WhereScalar(rows.SelectScalar(joined, expr.AsSelected(expr.Lower(caption))),
		expr.Eq(expr.At(fields, Records.Active), true), expr.IsNotNull(expr.AtNullable(nullable, Records.Score)))).OrderBy(expr.Asc(caption), expr.Asc(expr.At(fields, Records.ID)))
	if _, err := rows.CompileRead(text); err != nil {
		return err
	}
	_, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(joined, expr.AsSelected(expr.Row(expr.Coalesce(expr.AtNullable(nullable, Records.Payload), expr.At(fields, Records.Payload)))))))
	return err
}
