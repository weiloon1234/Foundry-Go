package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
)

type Form struct{ File string }

var _ = h.FilePart[Form]("file", func(input *Form) *string { return &input.File })
