package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type tableSource = querycore.TableSource

type joinKind = querycore.JoinKind

const innerJoin = querycore.InnerJoin

const leftJoin = querycore.LeftJoin

const rightJoin = querycore.RightJoin

const fullJoin = querycore.FullJoin

const crossJoin = querycore.CrossJoin

type joinNode = querycore.JoinNode

type orderNode = querycore.OrderNode

type selectNode = querycore.SelectNode

func orderNodes(orders []queryOrder) []orderNode { return querycore.OrderNodes(orders) }

func requalify(e expression, alias string) expression { return querycore.Requalify(e, alias) }

func requalifyValue(v valueExpression, alias string) valueExpression {
	return querycore.RequalifyValue(v, alias)
}
