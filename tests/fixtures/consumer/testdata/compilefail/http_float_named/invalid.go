package invalid

import (
	"foundry.test/consumer/httpquery"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type Longitude float32

func invalid(value Longitude) { _, _ = foundryhttp.FloatQuery[httpquery.Latitude]().Format(value) }
