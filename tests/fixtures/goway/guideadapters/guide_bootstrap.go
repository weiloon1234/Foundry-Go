package guideadapters

import (
	"context"

	"foundry.test/goway/models"

	"github.com/weiloon1234/Foundry-Go/application"
)

// doc-example:begin application-bootstrap-2
func modelApplication(ctx context.Context, settings application.Settings) (*application.App, error) {
	app, err := application.New(settings, application.WithTables(models.Tables()...)).Build(ctx)
	return app, err
}

// doc-example:end application-bootstrap-2
