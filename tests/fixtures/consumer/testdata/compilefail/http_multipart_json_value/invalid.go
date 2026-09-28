package invalid

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type Form struct{ Value int }

var _ = h.JSONPart[Form, string]("value", contract.JSON[string]{}, func(input *Form) *int { return &input.Value })
