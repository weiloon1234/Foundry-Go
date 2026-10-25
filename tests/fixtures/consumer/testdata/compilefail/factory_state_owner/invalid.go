package compilefail

import (
	"context"
	"foundry.test/consumer/models"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/factory"
)

func invalid() {
	records, _ := tooling.Records()
	var state factory.State[[]orm.Assignment[models.User]] = func(context.Context, []orm.Assignment[models.User]) ([]orm.Assignment[models.User], error) {
		return []orm.Assignment[models.User]{}, nil
	}
	_, _ = records.WithStates(state)
}
