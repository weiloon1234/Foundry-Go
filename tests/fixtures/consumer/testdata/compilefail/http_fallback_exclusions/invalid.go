package invalid

import h "github.com/weiloon1234/Foundry-Go/http"

var wrong = h.FallbackConfig{Exclude: []string{"/api"}}
