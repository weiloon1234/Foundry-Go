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

var _ = modelbinding.ByKey[httpkernel.UserPath, models.User, model.ID[models.User]](database.Executor(nil), models.QueryUsers().ForUpdate(), func(p httpkernel.UserPath) model.ID[models.User] { return p.User })
