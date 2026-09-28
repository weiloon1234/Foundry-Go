package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
)

type Form struct{ Files []string }

var _ = h.RepeatedFilePart[Form]("files", func(input *Form) *[]string { return &input.Files })
