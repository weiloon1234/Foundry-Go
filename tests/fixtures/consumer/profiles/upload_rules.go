package profiles

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
	imagingvalidation "github.com/weiloon1234/Foundry-Go/validation/imaging"
)

// doc-example:begin document-kinds
func DocumentRule() validation.Rule[foundryhttp.UploadedFile] {
	return validation.Bail(
		validation.FileMinSize[foundryhttp.UploadedFile](1),
		validation.FileKinds[foundryhttp.UploadedFile](validation.PDFKind(), validation.TextKind(), validation.ZIPContainerKind("docx")),
		validation.FileMaxSize[foundryhttp.UploadedFile](5<<20),
	)
}

// doc-example:end document-kinds

// doc-example:begin image-kinds
func ImageRule(engine *imaging.Engine) validation.Rule[foundryhttp.UploadedFile] {
	jpeg := imagingvalidation.JPEGKind()
	jpeg.Extension = "jpeg"
	return validation.Bail(
		validation.FileMinSize[foundryhttp.UploadedFile](1),
		validation.FileMaxSize[foundryhttp.UploadedFile](5<<20),
		imagingvalidation.KindsWithEngine[foundryhttp.UploadedFile](engine,
			validation.DimensionConstraints{MaxWidth: 10000, MaxHeight: 10000},
			imagingvalidation.JPEGKind(), jpeg, imagingvalidation.PNGKind(),
			imagingvalidation.WebPKind(), imagingvalidation.GIFKind(), imagingvalidation.AVIFKind()),
	)
}

// doc-example:end image-kinds

// doc-example:begin image-inspection-messages
func ImageRuleWithMessages(engine *imaging.Engine) validation.Rule[foundryhttp.UploadedFile] {
	checked := validation.WithImageInspection[foundryhttp.UploadedFile](
		imagingvalidation.NewEngineMeasurer[foundryhttp.UploadedFile](engine),
		func(image validation.ImageRules[foundryhttp.UploadedFile, imaging.Format]) validation.Rule[foundryhttp.UploadedFile] {
			matches := validation.WithMessage(image.Kinds(imagingvalidation.PNGKind()), "Must be a PNG image.")
			dimensions := validation.WithMessage(image.Dimensions(validation.DimensionConstraints{MaxWidth: 10000, MaxHeight: 10000}), "Image dimensions are too large.")
			return validation.Bail(matches, dimensions)
		},
	)
	return validation.Bail(validation.FileMinSize[foundryhttp.UploadedFile](1), validation.FileMaxSize[foundryhttp.UploadedFile](5<<20), checked)
}

// doc-example:end image-inspection-messages
