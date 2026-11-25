package guideadapters

import (
	"context"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/application"
)

func modelApplication(ctx context.Context, settings application.Settings) (*application.App, error) {
	// doc-example:begin application-bootstrap-2
	return application.New(settings, application.WithTables(models.Tables()...)).Build(ctx)
	// doc-example:end application-bootstrap-2
}
