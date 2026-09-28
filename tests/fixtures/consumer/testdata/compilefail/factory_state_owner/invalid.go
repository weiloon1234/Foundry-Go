package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func invalid() {
	records, _ := tooling.Records()
	var state factory.State[models.UserDraft] = func(context.Context, models.UserDraft) (models.UserDraft, error) { return models.UserDraft{}, nil }
	_, _ = records.WithStates(state)
}
