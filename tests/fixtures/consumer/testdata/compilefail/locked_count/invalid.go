package invalid

import "foundry.test/consumer/models"

var _, _ = models.QueryUsers().ForUpdate().Count(nil, nil)
