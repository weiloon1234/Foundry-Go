package validation

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// ImageFormat is an immutable, typed format identifier validated by its owner.
type ImageFormat interface {
	~string
	// Validate rejects unknown format declarations.
	Validate() error
}

// ImageKind correlates file metadata with an inspector's concrete format type.
// An empty ContentType skips MIME sniff matching for formats the upload detector
// cannot identify, such as AVIF; the inspected format is still required.
type ImageKind[Format ImageFormat] struct {
	FileKind
	// Format is the container encoding required from the inspector.
	Format Format `json:"format"`
}

// ImageInspection contains the format and orientation-corrected dimensions from
// one bounded container inspection. It does not prove compressed pixels decode.
type ImageInspection[Format ImageFormat] struct {
	// Size contains dimensions in displayed orientation.
	Size ImageSize
	// Format identifies the inspected container encoding.
	Format Format
}

// ImageInspector supplies a single bounded observation for kind and dimension
// validation. Unsupported, malformed and oversized images return ok=false;
// I/O and internal inspection failures return an execution error.
type ImageInspector[F any, Format ImageFormat] interface {
	// Validate checks configuration before any file is opened.
	Validate() error
	// Inspect returns displayed dimensions and the detected container format.
	Inspect(context.Context, F) (info ImageInspection[Format], ok bool, err error)
}

// ImageKinds correlates extension, detected MIME (when declared), and inspected
// format, then checks dimensions from that same inspection. A zero constraints
// value omits application dimension bounds; inspector resource limits still apply.
// This server-only leaf has one message for kind or dimension rejection and
// supports WithTranslation and WithMessage. Use the imaging adapter for uploads.
func ImageKinds[F FileValue, Format ImageFormat](inspector ImageInspector[F, Format], constraints DimensionConstraints, allowed ...ImageKind[Format]) Rule[F] {
	if inspector == nil {
		return failed[F](invalid("image inspector is missing"))
	}
	if constraints != (DimensionConstraints{}) {
		if err := constraints.Validate(); err != nil {
			return failed[F](err)
		}
	}
	if err := callback.Isolated("validation image inspector declaration", inspector.Validate); err != nil {
		return failed[F](safeerror.WrapFault(fault.Invalid, "invalid image inspector", err))
	}
	if len(allowed) == 0 || len(allowed) >= maxRuleNodes {
		return failed[F](invalid("image validation requires a bounded nonempty kind list"))
	}
	files := make([]FileKind, len(allowed))
	for i, kind := range allowed {
		files[i] = kind.FileKind
	}
	files, err := normalizeFileKinds(files, true)
	if err != nil {
		return failed[F](err)
	}
	kinds := make([]ImageKind[Format], len(allowed))
	for i, kind := range allowed {
		if !validText(string(kind.Format), false) {
			return failed[F](invalid("image kind requires a format"))
		}
		if err := callback.Isolated("validation image format declaration", kind.Format.Validate); err != nil {
			return failed[F](safeerror.WrapFault(fault.Invalid, "invalid image format", err))
		}
		kinds[i] = ImageKind[Format]{FileKind: files[i], Format: kind.Format}
	}
	// Convert format identifiers to native strings before metadata encoding so
	// application-defined JSON methods are never invoked at this boundary.
	metadata := make([]struct {
		FileKind
		Format string `json:"format"`
	}, len(kinds))
	for i, kind := range kinds {
		metadata[i].FileKind, metadata[i].Format = kind.FileKind, string(kind.Format)
	}
	parameters := append([]Parameter{parameter("kinds", metadata)}, constraints.parameters()...)
	rule := valueRule(Spec{ID: "foundry.image_kinds", Parameters: parameters}, true, func(s *execution, file F) (bool, error) {
		if file.IsZero() {
			return false, nil
		}
		extension := file.Extension()
		if !textValue(s, extension) {
			return false, nil
		}
		extension, ok := fileExtension(extension)
		if !ok {
			return false, nil
		}
		// Reject metadata mismatches before opening the upload. Empty MIME kinds
		// deliberately do not depend on the upload detector's AVIF support.
		media, readMedia := "", false
		matches := func(kind ImageKind[Format]) bool {
			if kind.Extension != extension {
				return false
			}
			if kind.ContentType == "" {
				return true
			}
			if !readMedia {
				readMedia = true
				input := file.ContentType()
				if textValue(s, input) {
					media, _ = fileMediaType(input, true)
				}
			}
			return kind.ContentType == media
		}
		candidate := false
		for _, kind := range kinds {
			candidate = matches(kind) || candidate
		}
		if !candidate || s.err != nil {
			return false, nil
		}
		info, ok, err := inspector.Inspect(s.ctx, file)
		if err != nil || !ok {
			return false, err
		}
		for _, kind := range kinds {
			if matches(kind) && info.Format == kind.Format {
				return constraints.accepts(info.Size), nil
			}
		}
		return false, nil
	})
	rule.callbacks = rule.err == nil
	return rule
}
