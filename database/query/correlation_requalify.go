package query

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

func requalifyCorrelation(q subquery, alias string) subquery {
	return querycore.RequalifyCorrelation(q, alias)
}
