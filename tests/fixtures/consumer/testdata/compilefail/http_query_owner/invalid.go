package invalid

import (
	"context"
	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func Encode() {
	_, _ = httpquery.SearchParameters.Encode(context.Background(), httpquery.OtherInput{}, foundryhttp.QueryLimits{})
}
