package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
)

// CaptureOrigin preserves provenance for an event/job payload without retaining
// a request model, bearer token or auth scope. It does not authorize that work.
func CaptureOrigin(ctx context.Context, guard auth.Guard[models.User]) (attribution.Origin, error) {
	return guard.Origin(ctx)
}

// AttributeOperation is useful in a CLI/worker's explicitly verified scope.
// Typed HTTP endpoints already attach their guard's origin before model binding.
func AttributeOperation(ctx context.Context, guard auth.Guard[models.User]) (context.Context, error) {
	return guard.WithAttribution(ctx)
}
