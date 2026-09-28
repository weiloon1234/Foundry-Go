package invalid

import "github.com/weiloon1234/Foundry-Go/auth"

func invalid(p auth.Permission[int], key string) { _ = p.WithLabelKey(key) }
