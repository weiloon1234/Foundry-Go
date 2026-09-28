package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type first struct{}
type second struct{}

var _ = foundryhttp.MergeQueries[first](foundryhttp.DefineQuery[first](), foundryhttp.DefineQuery[second]())
