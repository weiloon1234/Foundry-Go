package recursivequeries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin recursive-aliases
type ParentAlias struct{}
type ChildAlias struct{}

// doc-example:end recursive-aliases

// doc-example:begin model-recursive-ctes
func Descendants(self rows.RecursiveSelf[models.User]) rows.RecordSource[models.User] {
	parent := rows.AsRecord[ParentAlias](self, "parent")
	child := rows.As[ChildAlias](models.Users.Query(), "child")
	p, c := rows.Scope(parent), rows.Scope(child)
	joined := rows.InnerJoin(child, parent, rows.OnEqual(expr.At(c, models.Users.IntroducerID), expr.NullableRow(expr.At(p, models.Users.ID))))
	return rows.SelectRecord(joined, rows.LeftScope(joined, rows.Scope(child)))
}

// doc-example:end model-recursive-ctes
