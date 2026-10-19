package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func orderedCTEs(root selectNode) ([]*cteNode, error) { return querycore.OrderedCTEs(root) }
