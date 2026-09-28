package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmodels"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ context.Context
var _ httpdto.UserResponse
var _ database.Executor
var _ model.ID[models.User]
var _ httpkernel.UserPath
var _ foundryhttp.NoQuery
var _ = httpmodels.Show

var _ = modelbinding.Bind(httpmodels.Show, modelbinding.Resolver[httpkernel.UserPath, models.User]{}).Handle(func(context.Context, httpmodels.ShowRequest) (models.User, error) { return models.User{}, nil })
