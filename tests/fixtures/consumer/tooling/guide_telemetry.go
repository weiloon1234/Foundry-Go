package tooling

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// doc-example:begin persisted-telemetry-2
func recordBrowserError(ctx context.Context, services application.Services) error {
	engine, err := services.Telemetry()
	if err != nil {
		return err
	}
	return engine.RecordBrowser(ctx, telemetry.BrowserError{
		Portal: "admin", Release: "release-1", Code: telemetry.BrowserCodeTypeError,
		Frames: []telemetry.BrowserFrame{{File: "admin/_assets/app.js", Line: 1}},
	})

}

// doc-example:end persisted-telemetry-2
