package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ReorderUnwindowed validates report ordering before removing the outer orders.
func ReorderUnwindowed(node SelectNode, orders int) (SelectNode, error) {
	if node.Limit.IsSet() || node.Offset != 0 || node.Distinct.Kind == DistinctOn || orders == 0 {
		return node, fault.New(fault.Invalid, "report ordering requires an unwindowed source without outer DISTINCT ON and explicit orders")
	}
	node.Orders = nil
	return node, nil
}

// ResultPageWindow applies a validated page request to an explicitly ordered result.
func ResultPageWindow(node SelectNode, offset, size int, lookahead bool) (SelectNode, error) {
	if node.Limit.IsSet() || node.Offset != 0 || len(node.Orders) == 0 {
		return node, fault.New(fault.Invalid, "result pagination requires explicit ordering and no outer Limit/Offset")
	}
	if lookahead {
		size++
	}
	node.Limit = value.Set(size)
	node.Offset = offset
	return node, nil
}
