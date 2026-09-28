package invalid

import (
	"context"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var bad = h.StreamFrom(func(context.Context) (h.DownloadContent, error) { return h.DownloadContent{}, nil })
