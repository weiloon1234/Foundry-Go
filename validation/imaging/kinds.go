package imaging

import (
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// Kind correlates an extension, optional detected MIME type, and engine format.
// Empty ContentType means engine inspection alone establishes the content kind.
type Kind = validation.ImageKind[imaging.Format]

// JPEGKind declares jpg with JPEG content. Set Extension to jpeg to allow that alias.
func JPEGKind() Kind { return imageKind(imaging.JPEG) }

// PNGKind declares png with PNG content.
func PNGKind() Kind { return imageKind(imaging.PNG) }

// WebPKind declares webp with WebP content.
func WebPKind() Kind { return imageKind(imaging.WebP) }

// GIFKind declares gif with GIF content.
func GIFKind() Kind { return imageKind(imaging.GIF) }

// AVIFKind declares avif using container inspection, without requiring the
// bounded upload MIME sniff to recognize AVIF.
func AVIFKind() Kind {
	kind := imageKind(imaging.AVIF)
	kind.ContentType = ""
	return kind
}

func imageKind(format imaging.Format) Kind {
	return Kind{FileKind: validation.FileKind{Extension: format.Extension(), ContentType: format.MediaType()}, Format: format}
}

// Kinds validates correlated file kinds and dimensions with one bounded portable
// inspection. Zero constraints omit application dimension checks; engine byte,
// pixel and other resource limits remain enforced. Wrap this leaf in
// WithTranslation to retain an application's image message.
func Kinds[F File](limits imaging.Limits, constraints validation.DimensionConstraints, allowed ...Kind) validation.Rule[F] {
	return validation.ImageKinds[F](NewMeasurer[F](limits), constraints, allowed...)
}

// KindsWithEngine borrows the configured engine for a single kind and dimension
// inspection, including native formats when available. The caller owns its lifetime.
func KindsWithEngine[F File](engine *imaging.Engine, constraints validation.DimensionConstraints, allowed ...Kind) validation.Rule[F] {
	return validation.ImageKinds[F](NewEngineMeasurer[F](engine), constraints, allowed...)
}
