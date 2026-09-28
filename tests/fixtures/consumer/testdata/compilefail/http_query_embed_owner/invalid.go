package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type inner struct{}
type outer struct{ Value inner }
type other struct{ Value inner }

var _ = foundryhttp.EmbedQuery[outer, inner](foundryhttp.DefineQuery[inner](), func(q *other) *inner { return &q.Value })
