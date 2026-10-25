package invalid

import (
	"foundry.test/consumer/models"
)

var _, _ = models.Countries.Query().ForUpdate().SkipLocked().Where(models.Countries.Name.Eq("x")).Where(models.Countries.Code.Eq(models.LocationCode("A"))).First(nil, nil)
