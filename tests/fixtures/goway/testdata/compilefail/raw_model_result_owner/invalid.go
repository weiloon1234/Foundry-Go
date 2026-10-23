package invalid

import "foundry.test/goway/models"

func invalid() ([]models.User, error) { return models.Orders.Raw(nil, nil, "SELECT * FROM orders") }
