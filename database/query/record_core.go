package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func selectRecordNode(node selectNode, table string, columns []columnData, hasDecoder bool) (selectNode, error) {
	return querycore.SelectRecordNode(node, table, columns, hasDecoder)
}
