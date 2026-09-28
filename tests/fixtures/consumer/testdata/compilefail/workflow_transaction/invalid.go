package invalid

import (
	"context"
	fixture "foundry.test/consumer/teamworkflow"
	"github.com/weiloon1234/Foundry-Go/database"
)

func bad(service *fixture.Service, ctx context.Context, db *database.DB) {
	service.Submit(ctx, db, fixture.Actor{}, fixture.SubmissionRequest{})
}
