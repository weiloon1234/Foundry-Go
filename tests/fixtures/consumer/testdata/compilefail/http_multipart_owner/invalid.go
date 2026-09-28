package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
)

type First struct{ File h.UploadedFile }
type Second struct{ File h.UploadedFile }

var _ = h.DefineMultipart[First](h.FilePart("file", func(input *Second) *h.UploadedFile { return &input.File }))
