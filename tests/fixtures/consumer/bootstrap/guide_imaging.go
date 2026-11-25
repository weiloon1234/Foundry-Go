package bootstrap

import (
	"context"
	"image/color"
	"io"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin imaging-1
func watermarkedPhoto(ctx context.Context, engine *imaging.Engine, watermarkReader, photoReader io.Reader) error {
	watermark, err := engine.Process(ctx, watermarkReader,
		imaging.NewPlan().Fit(160, 80, false).Format(imaging.PNG))
	if err != nil {
		return err
	}
	plan := imaging.NewPlan().
		Contain(1200, 630, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
		Insert(watermark, imaging.Placement{
			Position: imaging.BottomRight, X: -16, Y: -16,
			Opacity: value.Set(0.75),
		}).Format(imaging.WebP)
	result, err := engine.Process(ctx, photoReader, plan)
	if err != nil {
		return err
	}
	_ = result
	return err
}

// doc-example:end imaging-1

// doc-example:begin imaging-2
func captionPlan() error {
	font, err := imaging.BuiltinFont(imaging.FontBold)
	if err != nil {
		return err
	}
	plan := imaging.NewPlan().
		Contain(320, 160, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
		Rectangle(8, 100, 304, 52, imaging.ShapeStyle{
			Fill: color.NRGBA{R: 20, G: 40, B: 70, A: 255},
		}).
		Text("Foundry profile", imaging.TextOptions{
			Font: font, Size: 24,
			Color:    color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			Position: imaging.Bottom, Y: -20,
			MaxWidth: 280, Align: imaging.AlignCenter,
		}).Format(imaging.PNG)
	_ = plan
	return nil
}

// doc-example:end imaging-2

// doc-example:begin imaging-3
func animatedPhoto(ctx context.Context, engine *imaging.Engine, uploadReader io.Reader) error {
	plan := imaging.NewPlan().Frames(imaging.PreserveAnimation).
		Fit(320, 320, false).Format(imaging.PNG)
	result, err := engine.Process(ctx, uploadReader, plan)
	if err != nil {
		return err
	}

	capability, known := engine.Capabilities().ForFormat(imaging.PNG)
	// capability.Read, Write, ReadAnimation, WriteAnimation are separate flags.
	_ = result
	_ = capability
	_ = known
	return err
}

// doc-example:end imaging-3

// doc-example:begin imaging-4
func encodingPlans() (imaging.Plan, imaging.Plan) {
	pngPlan := imaging.NewPlan().Format(imaging.PNG).
		PNGCompression(imaging.PNGBestCompression)
	avifPlan := imaging.NewPlan().Format(imaging.AVIF).
		AVIFQuality(65).AVIFSpeed(6).AVIFAlphaQuality(100)
	return pngPlan, avifPlan
}

// doc-example:end imaging-4

// doc-example:begin imaging-5
func enableImages(settings application.Settings) application.Settings {
	settings.Image.Enabled = true
	return settings
}

// doc-example:end imaging-5

// doc-example:begin imaging-6
func colorManagedPlan() imaging.Plan {
	plan := imaging.NewPlan().
		ToSRGB().
		SmartFill(512, 512, false, imaging.CropAttention).
		Metadata(imaging.PreserveColorProfile).
		Format(imaging.JPEG)
	return plan
}

// doc-example:end imaging-6
