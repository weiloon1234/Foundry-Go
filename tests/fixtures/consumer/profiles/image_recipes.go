package profiles

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

// doc-example:begin configured-image-engine
func ImageSettings() application.Settings {
	settings := application.DefaultSettings()
	settings.Image.Enabled = true
	settings.Image.Config.MaxActive = 2
	return settings
}

func ProfileImages(services application.Services) (*imaging.Engine, error) {
	return services.Image()
}

// doc-example:end configured-image-engine

// doc-example:begin process-image
func ProcessCover(ctx context.Context, engine *imaging.Engine, source io.Reader) (imaging.Result, error) {
	plan := imaging.NewPlan().Fill(1200, 630, true).Format(imaging.WebP)
	return engine.Process(ctx, source, plan)
}

// doc-example:end process-image
