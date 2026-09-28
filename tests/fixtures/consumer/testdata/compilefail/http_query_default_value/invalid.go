package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type input struct{ Size int }

var _ = foundryhttp.DefaultQueryParam[input, int]("size", foundryhttp.IntegerQuery[int](), "wrong", func(q *input) *int { return &q.Size })
