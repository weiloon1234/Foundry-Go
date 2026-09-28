package invalid

import (
	"context"
	h "github.com/weiloon1234/Foundry-Go/http"
	"io"
)

var wrong = h.DownloadFrom(func(context.Context) (io.ReadSeekCloser, error) { return nil, nil })
