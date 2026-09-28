package invalid

import (
	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ = foundryhttp.Query[httpquery.OtherInput](httpquery.SearchParameters)
