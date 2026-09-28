package invalid

import (
	"context"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
	"foundry.test/consumer/httpsigned"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"time"
)

var links httpsigned.Links
var _, _ = links.Preview.URL(context.Background(), foundryhttp.Origin("https://example.test"), httpkernel.UserPath{}, httpquery.OtherInput{}, time.Time{})
