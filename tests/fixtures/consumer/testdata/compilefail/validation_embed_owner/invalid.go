package invalid

import "github.com/weiloon1234/Foundry-Go/validation"

var _ = validation.Embed[int, string](validation.Min(1), func(int) string { return "" })
