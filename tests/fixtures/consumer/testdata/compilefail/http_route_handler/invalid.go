package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"net/http"
)

type UserPath struct{ Name string }
type OrderPath struct{ Name string }

func invalid(route foundryhttp.Route[UserPath]) {
	_ = route.HandleRaw(func(http.ResponseWriter, *http.Request, OrderPath) {})
}
