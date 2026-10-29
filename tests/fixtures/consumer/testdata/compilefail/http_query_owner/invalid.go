package invalid

import (
	"context"

	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func Encode() {
	_, _ = foundryhttp.EncodeQuery[httpquery.SearchInput](httpquery.SearchParameters, context.Background(), httpquery.OtherInput{}, foundryhttp.QueryLimits{})
}
