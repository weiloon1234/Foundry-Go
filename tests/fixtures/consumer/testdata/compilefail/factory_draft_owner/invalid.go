package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func invalid() {
	_, _ = factory.New[models.User](func(context.Context, factory.Sequence) (models.WriteRecordDraft, error) {
		return models.WriteRecordDraft{}, nil
	})
}
