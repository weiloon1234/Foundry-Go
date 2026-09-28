package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

func invalid(value float64) { _, _ = foundryhttp.FloatPath[float32]().Format(value) }
