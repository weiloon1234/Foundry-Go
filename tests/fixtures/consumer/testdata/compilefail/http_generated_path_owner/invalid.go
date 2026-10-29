package invalid

import (
	"foundry.test/consumer/httpkernel"
	http "github.com/weiloon1234/Foundry-Go/http"
)

var _, _ = http.RouteURL[httpkernel.UserPath](httpkernel.ShowUser, httpkernel.AssetPath{})
