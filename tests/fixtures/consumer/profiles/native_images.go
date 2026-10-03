package profiles

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/imaging"
)

// NativeImageSettings enables the optional backend through ordinary application
// configuration. The executable is built with cgo and -tags foundry_vips.
func NativeImageSettings() application.Settings {
	settings := application.DefaultSettings()
	settings.HTTP.Enabled = false
	settings.Image.Enabled = true
	settings.Image.Config.Backend = imaging.LibvipsBackend
	return settings
}

// NativePortraitPlan uses ICC color conversion and attention-guided cropping.
// StripMetadata is the default; published portraits do not carry EXIF GPS.
func NativePortraitPlan() imaging.Plan {
	return imaging.NewPlan().
		ToSRGB().
		SmartFill(64, 64, true, imaging.CropAttention).
		Format(imaging.JPEG).
		JPEGQuality(85)
}
