package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

var _ = foundryhttp.CSPPolicy{ScriptSrc: []foundryhttp.CSPSource{foundryhttp.FrameDeny}}
