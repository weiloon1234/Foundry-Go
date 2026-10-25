package invalid

import "foundry.test/consumer/models"

func invalid() ([]models.Order, error) { return models.Users.Query().ForUpdate().All(nil, nil) }
