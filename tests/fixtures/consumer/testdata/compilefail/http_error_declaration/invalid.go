package invalid

import (
	"errors"

	"foundry.test/consumer/httpendpoints"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _ = http.ConfigureEndpoint(httpendpoints.Reserve, http.Errors(errors.New("unsafe public text")))
