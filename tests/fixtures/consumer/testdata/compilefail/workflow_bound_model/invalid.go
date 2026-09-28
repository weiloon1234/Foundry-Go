package invalid

import (
	"context"
	fixture "foundry.test/consumer/teamworkflow"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
)

func bad(service *fixture.Service, ctx context.Context) {
	service.Patch(ctx, fixture.Actor{}, modelbinding.Input[fixture.ProjectPath, http.NoQuery, fixture.Patch, fixture.Project]{})
}
