package invalid

import "foundry.test/consumer/models"

func wrong() {
	_ = models.QueryUsers().EachChunked(nil, nil, 2, func(models.Order) error { return nil })
}
