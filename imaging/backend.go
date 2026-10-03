package imaging

import (
	"context"
	"image"
	"slices"
)

// Backend selects the engine's codec extensions. LibvipsBackend requires a
// build with cgo and the foundry_vips tag, plus the libvips development library.
// Selecting an unavailable backend fails explicitly at construction.
type Backend uint8

const (
	PortableBackend Backend = iota
	LibvipsBackend
)

// CropInterest selects the libvips heuristic used by SmartFill.
type CropInterest uint8

const (
	CropAttention CropInterest = iota
	CropEntropy
)

// SmartFill resizes to cover the requested canvas and selects a crop using
// visual attention or entropy. Requires LibvipsBackend. Like Fill, it rejects
// an insufficient source size when upscale is false.
func (p Plan) SmartFill(width, height int, upscale bool, interest CropInterest) Plan {
	return p.append(step{kind: resizeSmart, width: width, height: height, upscale: upscale, interest: interest})
}

// ToSRGB converts an embedded ICC profile to 8-bit sRGB before applying the
// plan. Images without a profile use their declared color interpretation.
// Requires LibvipsBackend; malformed profiles fail instead of being ignored.
func (p Plan) ToSRGB() Plan { p.srgb = true; return p }

// nativeSource owns the native image and its encoded backing storage until all
// decoding, metadata transfer and encoding has completed. No native handles are
// exposed to applications or retained in Result.
type nativeSource interface {
	decode(context.Context, Plan, Limits) (image.Image, error)
	encode(context.Context, *boundedOutput, image.Image, Plan, Format, Limits) error
	close()
}

func (e *Engine) ValidatePlan(p Plan) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.srgb && !e.capabilities.ColorManagement || p.metadata != StripMetadata && !e.capabilities.MetadataPreservation {
		return invalid("image plan requires the libvips color or metadata capability")
	}
	for _, s := range p.steps {
		if s.kind == resizeSmart && !e.capabilities.SmartCrop {
			return invalid("smart cropping requires the libvips backend")
		}
	}
	if p.output != "" {
		return e.validateOutput(p, p.output)
	}
	return nil
}

func (e *Engine) validateOutput(p Plan, format Format) error {
	capability, ok := e.capabilities.ForFormat(format)
	if !ok || !capability.Write {
		return invalid("image output format is unavailable in this engine")
	}
	if p.nativeOutput(format) && format == AVIF && p.encoding.avifAlphaQuality.IsSet() {
		return invalid("native AVIF metadata output cannot set independent alpha quality")
	}
	if p.metadata != StripMetadata && !capability.WriteMetadata {
		return invalid("image output format cannot preserve metadata in this engine")
	}
	return nil
}

// Inspect examines input with this engine's codecs, limits and admission owner.
// The package-level Inspect function always uses portable codecs.
func (e *Engine) Inspect(ctx context.Context, data []byte) (Info, error) {
	if err := e.Validate(); err != nil {
		return Info{}, err
	}
	var info inspection
	err := e.calls.Run(ctx, "inspect image", func(ctx context.Context) error {
		var err error
		info, err = inspectForBackend(ctx, data, e.config.Limits, e.config.Backend)
		return err
	})
	if err != nil {
		return Info{}, err
	}
	return info.Info, nil
}

func inspectForBackend(ctx context.Context, data []byte, limits Limits, backend Backend) (inspection, error) {
	if err := ctx.Err(); err != nil {
		return inspection{}, err
	}
	if backend == LibvipsBackend {
		if format := nativeInputFormat(data); format != "" {
			return inspectNative(ctx, data, format, limits)
		}
	}
	return inspect(data, limits)
}

func (p Plan) nativePixels(info inspection) bool {
	return info.native || p.srgb || p.metadata != StripMetadata
}
func (p Plan) nativeOutput(format Format) bool { return format.native() || p.metadata != StripMetadata }

func nativeFormats() [4]Format { return [4]Format{HEIF, JPEG2000, JPEGXL, SVG} }
func (f Format) native() bool  { formats := nativeFormats(); return slices.Contains(formats[:], f) }
