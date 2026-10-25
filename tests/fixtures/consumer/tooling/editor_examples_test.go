package tooling_test

import (
	"context"

	"foundry.test/consumer/models"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/database"
)

// The editor example uses the assignment factory without generated model APIs.
func createRecordExample(ctx context.Context, writer database.Transactor) (models.WriteRecord, error) {
	records, err := tooling.Records()
	if err != nil {
		return models.WriteRecord{}, err
	}
	return records.Create(ctx, writer)
}
