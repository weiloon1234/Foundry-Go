package invalid

import "foundry.test/consumer/models"

var _, _ = models.QueryUsers().CreateMany(nil, nil, []models.OrderDraft{})
