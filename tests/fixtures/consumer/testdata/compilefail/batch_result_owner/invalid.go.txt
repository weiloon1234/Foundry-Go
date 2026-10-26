package invalid

import "foundry.test/consumer/models"

func wrong() {
	items, _ := models.QueryUsers().CreateMany(nil, nil, []models.UserDraft{})
	var _ []models.Order = items
}
