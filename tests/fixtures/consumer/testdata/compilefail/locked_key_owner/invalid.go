package invalid

import "foundry.test/consumer/models"

var _, _ = models.QueryCountries().ForUpdate().SkipLocked().Where(models.CountryFields().Name.Eq("x")).Find(nil, nil, models.LocationCode("A"))
