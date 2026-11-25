package validationrules

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

// doc-example:begin avatar-upload-rules
func avatarUploadRules() validation.Rule[foundryhttp.UploadedFile] {
	return validation.Bail(
		validation.FileMaxSize[foundryhttp.UploadedFile](5<<20),
		validation.FileContentTypes[foundryhttp.UploadedFile]("image/png", "image/jpeg"),
		imagingvalidation.Dimensions[foundryhttp.UploadedFile](imaging.DefaultLimits(),
			validation.DimensionConstraints{MinWidth: 128, MaxWidth: 4096, RatioWidth: 1, RatioHeight: 1}),
	)
}

// doc-example:end avatar-upload-rules

// doc-example:begin engine-upload-dimensions
func engineUploadDimensions(engine *imaging.Engine) validation.Rule[foundryhttp.UploadedFile] {
	return imagingvalidation.DimensionsWithEngine[foundryhttp.UploadedFile](engine,
		validation.DimensionConstraints{MaxWidth: 4096, MaxHeight: 4096})
}

// doc-example:end engine-upload-dimensions
