package invalid

import "foundry.test/consumer/models"

func invalid() ([]models.Order, error) { return models.QueryUsers().ForUpdate().All(nil, nil) }
