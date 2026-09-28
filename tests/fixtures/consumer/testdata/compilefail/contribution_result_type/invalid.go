package invalid

import "github.com/weiloon1234/Foundry-Go/foundation"

func invalidResult(s foundation.Resolver) {
	var items []string
	items, _ = foundation.ResolveAll[int](s)
	_ = items
}
