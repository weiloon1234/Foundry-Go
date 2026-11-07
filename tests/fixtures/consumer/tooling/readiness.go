package tooling

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/application"
)

// doc-example:begin feature-readiness
func WithDatabaseReadiness(builder *application.Builder) *application.Builder {
	return builder.Features(func(services application.Services) (application.FeatureDeclarations, error) {
		db, err := services.Database()
		if err != nil {
			return application.FeatureDeclarations{}, err
		}
		return application.FeatureDeclarations{Readiness: db.ReadinessProbes("database.primary", "database.read")}, nil
	})
}

// doc-example:end feature-readiness

// doc-example:begin pause-admission
func Pause(app *application.App, paused bool) {
	app.Maintenance().Set(paused)
}

// doc-example:end pause-admission

// doc-example:begin storage-probe
func ProbeStorage(ctx context.Context, builder *application.Builder, output io.Writer) error {
	return application.ProbeStorage(ctx, builder, output)
}

// doc-example:end storage-probe
