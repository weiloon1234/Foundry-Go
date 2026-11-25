package transactionqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type claimedPair = rows.TransactionCorrelation[rows.TransactionAlias[claimedAlias, Record], rows.TransactionAlias[otherAlias, Record]]

// claimedPairs builds the guide's parent, child and their correlation.
func claimedPairs() (rows.TransactionAliased[claimedAlias, Record], rows.TransactionAliased[otherAlias, Record], rows.TransactionCorrelatedSource[rows.TransactionAlias[claimedAlias, Record], rows.TransactionAlias[otherAlias, Record]], recordExpressions[claimedPair], recordExpressions[claimedPair]) {
	// doc-example:begin transaction-correlations
	parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.In(Records.ID, 1, 3)))), "parent")
	child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
	c = c.Where(expr.Greater(f.ID, p.ID))
	// doc-example:end transaction-correlations
	return parent, child, c, p, f
}
