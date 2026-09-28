package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Input struct{ Page value.Optional[int] }

var _ = foundryhttp.OptionalQueryParam[Input, string]("page", foundryhttp.StringQuery[string](), func(q *Input) *value.Optional[int] { return &q.Page })
