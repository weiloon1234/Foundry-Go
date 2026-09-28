package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type Input struct{ Page int }

var _ = foundryhttp.QueryParam[Input, string]("page", foundryhttp.StringQuery[string](), func(q *Input) *int { return &q.Page })
