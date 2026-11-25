package validationrules

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

func avatarUploadRules() validation.Rule[foundryhttp.UploadedFile] {
	// doc-example:begin avatar-upload-rules
	avatar := validation.Bail(
		validation.FileMaxSize[foundryhttp.UploadedFile](5<<20),
		validation.FileContentTypes[foundryhttp.UploadedFile]("image/png", "image/jpeg"),
		imagingvalidation.Dimensions[foundryhttp.UploadedFile](imaging.DefaultLimits(),
			validation.DimensionConstraints{MinWidth: 128, MaxWidth: 4096, RatioWidth: 1, RatioHeight: 1}),
	)
	// doc-example:end avatar-upload-rules
	return avatar
}

func engineUploadDimensions(engine *imaging.Engine) validation.Rule[foundryhttp.UploadedFile] {
	// doc-example:begin engine-upload-dimensions
	dimensions := imagingvalidation.DimensionsWithEngine[foundryhttp.UploadedFile](engine,
		validation.DimensionConstraints{MaxWidth: 4096, MaxHeight: 4096})
	// doc-example:end engine-upload-dimensions
	return dimensions
}
