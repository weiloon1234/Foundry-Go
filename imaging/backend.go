package imaging

import (
	"context"
	"errors"
	"image"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Backend selects native discovery. AutoBackend is the default: portable codecs
// always work and an installed libvips runtime adds native capabilities.
// LibvipsBackend requires discovery to succeed at construction. PortableBackend
// disables native discovery. No custom build tag or libvips headers are needed.
type Backend uint8

const (
	PortableBackend Backend = iota
	LibvipsBackend
	AutoBackend
)

// ErrNativeUnavailable matches a missing, disabled or incompatible native runtime,
// or a requested native capability absent from the installed libvips build.
var ErrNativeUnavailable = errors.New("native image capability is unavailable")

func nativeFailure(message string) error {
	return fault.Wrap(fault.Invalid, message, ErrNativeUnavailable)
}
func nativeUnavailable() error {
	return nativeFailure("requested image feature requires an available libvips runtime")
}

// NativeError reports why this engine cannot use native features. It is nil
// when libvips is ready. Applications may inspect it during startup; an automatic
// engine remains usable for portable operations when native discovery fails.
func (e *Engine) NativeError() error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.config.Backend == PortableBackend {
		return nativeFailure("native imaging is disabled by PortableBackend")
	}
	return e.nativeErr
}

func (e *Engine) unavailableNativeCapability() error {
	if err := e.NativeError(); err != nil {
		return err
	}
	return nativeFailure("requested native image capability is unavailable in the loaded libvips runtime")
}

func (e *Engine) inspect(ctx context.Context, data []byte) (inspection, error) {
	if format := nativeInputFormat(data); format != "" {
		if capability, ok := e.capabilities.ForFormat(format); !ok || !capability.Read {
			return inspection{}, e.unavailableNativeCapability()
		}
	}
	info, err := inspectForBackend(ctx, data, e.config.Limits, e.capabilities.Backend)
	if errors.Is(err, ErrNativeUnavailable) {
		return inspection{}, e.unavailableNativeCapability()
	}
	return info, err
}

// CropInterest selects the libvips heuristic used by SmartFill.
type CropInterest uint8

const (
	CropAttention CropInterest = iota
	CropEntropy
)

// SmartFill resizes to cover the requested canvas and selects a crop using
// visual attention or entropy. Requires an available libvips runtime. Like Fill, it rejects
// an insufficient source size when upscale is false.
func (p Plan) SmartFill(width, height int, upscale bool, interest CropInterest) Plan {
	return p.append(step{kind: resizeSmart, width: width, height: height, upscale: upscale, interest: interest})
}

// ToSRGB converts an embedded ICC profile to 8-bit sRGB before applying the
// plan. Images without a profile use their declared color interpretation.
// Requires an available libvips runtime; malformed profiles fail instead of being ignored.
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
		return e.unavailableNativeCapability()
	}
	for _, s := range p.steps {
		if s.kind == resizeSmart && !e.capabilities.SmartCrop {
			return e.unavailableNativeCapability()
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
		if format.native() && format != SVG {
			return e.unavailableNativeCapability()
		}
		return invalid("image output format is unavailable in this engine")
	}
	if p.nativeOutput(format) && format == AVIF && p.encoding.avifAlphaQuality.IsSet() {
		return invalid("native AVIF metadata output cannot set independent alpha quality")
	}
	if p.metadata != StripMetadata && !capability.WriteMetadata {
		return e.unavailableNativeCapability()
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
		info, err = e.inspect(ctx, data)
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
	if format := nativeInputFormat(data); format != "" {
		if backend != LibvipsBackend {
			return inspection{}, nativeUnavailable()
		}
		return inspectNative(ctx, data, format, limits)
	}
	return inspect(data, limits)
}

func (p Plan) nativePixels(info inspection) bool {
	return info.native || p.srgb || p.metadata != StripMetadata
}
func (p Plan) nativeOutput(format Format) bool { return format.native() || p.metadata != StripMetadata }

func nativeFormats() [4]Format { return [4]Format{HEIF, JPEG2000, JPEGXL, SVG} }
func (f Format) native() bool  { formats := nativeFormats(); return slices.Contains(formats[:], f) }
