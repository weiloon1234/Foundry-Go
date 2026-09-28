package invalid

import foundryhttp "github.com/weiloon1234/Foundry-Go/http"

type UserPath struct{ Name string }
type OrderPath struct{ Name string }

func invalid(route foundryhttp.Route[UserPath], path OrderPath) {
	_, _ = route.URL(path)
}
