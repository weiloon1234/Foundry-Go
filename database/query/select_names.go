package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type selectNames = querycore.SelectNames

func namesInSelects(nodes ...selectNode) (*selectNames, error) {
	return querycore.NamesInSelects(nodes...)
}
