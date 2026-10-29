package invalid

import (
	"net/http"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type UserPath struct{ Name string }
type OrderPath struct{ Name string }

func invalid(route foundryhttp.Route[UserPath]) {
	_ = foundryhttp.HandleRaw[UserPath](route, func(http.ResponseWriter, *http.Request, OrderPath) {})
}
