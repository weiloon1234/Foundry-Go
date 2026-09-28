package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	h "github.com/weiloon1234/Foundry-Go/http"
	"time"
)

var endpoint authenticating.SignedOrderEndpoint

func invalid() {
	_, _ = endpoint.URL(context.Background(), h.Origin("https://example.test"), authenticating.OrderPath{}, h.NoBody{}, time.Now())
}
