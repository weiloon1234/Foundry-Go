package invalid

import "foundry.test/consumer/models"

var _, _ = models.Users.Query().ForUpdate().Count(nil, nil)
