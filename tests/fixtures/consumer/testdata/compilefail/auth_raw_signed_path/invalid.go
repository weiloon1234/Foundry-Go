package invalid

import (
	"context"
	"time"

	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
)

var route h.SignedAuthenticatedRoute[authenticating.OrderPath, models.User]

func invalid() {
	_, _ = h.SignedAuthenticatedRouteURL[authenticating.OrderPath, models.User](route, context.Background(), h.Origin("https://example.test"), h.NoPath{}, time.Now())
}
