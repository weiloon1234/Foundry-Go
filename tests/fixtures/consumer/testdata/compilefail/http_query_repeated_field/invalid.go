package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type Input struct{ Values []string }

var _ = foundryhttp.RepeatedQueryParam[Input, int, []int]("value", foundryhttp.IntegerQuery[int](), func(q *Input) *[]string { return &q.Values })
