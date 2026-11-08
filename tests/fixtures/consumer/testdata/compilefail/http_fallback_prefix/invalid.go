package invalid

import h "github.com/weiloon1234/Foundry-Go/http"

func wrong() { h.DefineFallback("wrong", "/", nil, h.DefaultFallbackConfig()) }
