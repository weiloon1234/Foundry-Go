package invalid

import (
	h "github.com/weiloon1234/Foundry-Go/http"
	"io"
	"strings"
)

var wrong = h.DownloadContent{Body: io.NopCloser(strings.NewReader("file"))}
