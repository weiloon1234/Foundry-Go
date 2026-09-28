package invalid

import (
	"context"
	"foundry.test/consumer/authenticating"
	"foundry.test/consumer/models"
	h "github.com/weiloon1234/Foundry-Go/http"
	"time"
)

var route h.SignedAuthenticatedRoute[authenticating.OrderPath, models.User]

func invalid() {
	_, _ = route.URL(context.Background(), h.Origin("https://example.test"), h.NoPath{}, time.Now())
}
