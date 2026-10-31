package imaging

import (
	"image/color"
	"math"
	"slices"
)

// Orientation selects whether input orientation metadata is applied or ignored.
type Orientation uint8

const (
	ApplyOrientation Orientation = iota
	IgnoreOrientation
)

// Frames selects whether animation is rejected, reduced to its first frame or preserved.
type Frames uint8

const (
	RejectAnimation Frames = iota
	FirstFrame
	PreserveAnimation
)

// Metadata selects retention after transformation. The default strips metadata.
// Preservation requires an available libvips runtime and an output with WriteMetadata support.
// PreserveMetadata includes EXIF (including GPS), XMP, IPTC and ICC where supported;
// obsolete EXIF orientation, dimensions and thumbnails are normalized on output.
type Metadata uint8

const (
	StripMetadata Metadata = iota
	PreserveMetadata
	PreserveColorProfile
)

// Rotation specifies a supported rotation angle in degrees.
type Rotation uint16

const (
	Rotate90  Rotation = 90
	Rotate180 Rotation = 180
	Rotate270 Rotation = 270
)

type stepKind uint8

const (
	resizeExact stepKind = iota + 1
	resizeFit
	resizeFill
	crop
	blur
	grayscale
	rotate
	flipHorizontal
	flipVertical
	brightness
	contrast
	resizeDown
	resizeWidth
	resizeHeight
	resizePad
	resizeCanvas
	resizeCanvasRelative
	cropPositioned
	rotateDegrees
	invert
	gamma
	saturation
	hue
	sepia
	threshold
	pixelate
	sharpen
	insert
	mask
	drawPath
	drawText
	resizeSmart
)

type step struct {
	kind                stepKind
	width, height, x, y int
	number              float64
	upscale             bool
	position            Position
	background          color.NRGBA
	resampling          Resampling
	amount, threshold   float64
	layer               Result
	placement           Placement
	interest            CropInterest
	maskMode            MaskMode
	path                Path
	style               ShapeStyle
	text                *textSpec
}

// Plan holds copied values and immutable image results, never callbacks or
// caller-owned mutable image buffers.
// Methods return independent plans; a zero plan has no transforms and keeps the
// detected format. Quality applies only to JPEG, including a detected JPEG.
type Plan struct {
	steps          []step
	output         Format
	quality        int
	avifQuality    int
	encoding       encodingOptions
	nativeEncoding nativeEncoding
	background     color.NRGBA
	flatten        bool
	orientation    Orientation
	frames         Frames
	metadata       Metadata
	resampling     Resampling
	srgb           bool
}

const MaxTransforms = 32

// String and GoString redact prepared layer bytes from routine/debug formatting.
// Format is already the fluent output-format method, so Plan is not a Formatter.
func (Plan) String() string { return "image plan" }

// GoString returns a label without exposing prepared layer bytes.
func (Plan) GoString() string { return "image plan" }

// NewPlan returns a zero plan with no transforms and the detected input format.
func NewPlan() Plan               { return Plan{} }
func (p Plan) append(s step) Plan { p.steps = append(slices.Clone(p.steps), s); return p }

// Format returns a plan with the selected output format.
func (p Plan) Format(f Format) Plan { p.output = f; return p }

// JPEGQuality returns a plan with JPEG quality; zero selects the default quality.
func (p Plan) JPEGQuality(q int) Plan { p.quality = q; return p }

// AVIFQuality selects AVIF quality 1..100 (100 uses the lowest quantizer).
// RGB conversion and chroma subsampling still apply; it requires an
// explicit AVIF output. The default is DefaultAVIFQuality.
func (p Plan) AVIFQuality(q int) Plan { p.avifQuality = q; return p }

// Background flattens the result over an opaque color before encoding, so a
// format without alpha (JPEG) does not render transparent pixels as black.
// It applies to every output format when set.
func (p Plan) Background(c color.NRGBA) Plan { p.background, p.flatten = c, true; return p }

// Orientation returns a plan with the selected input orientation policy.
func (p Plan) Orientation(o Orientation) Plan { p.orientation = o; return p }

// Frames returns a plan with the selected animation handling policy.
func (p Plan) Frames(f Frames) Plan { p.frames = f; return p }

// Metadata returns a plan with the selected metadata retention policy.
func (p Plan) Metadata(m Metadata) Plan { p.metadata = m; return p }

// Resize returns a plan resizing exactly to the dimensions, allowing enlargement.
func (p Plan) Resize(width, height int) Plan {
	return p.append(step{kind: resizeExact, width: width, height: height, upscale: true})
}

// Fit returns a plan resizing proportionally within the dimensions, allowing
// enlargement only when upscale is true.
func (p Plan) Fit(width, height int, upscale bool) Plan {
	return p.append(step{kind: resizeFit, width: width, height: height, upscale: upscale})
}

// Fill returns a plan resizing to cover the dimensions and cropping at the center;
// insufficient source size fails when upscale is false.
func (p Plan) Fill(width, height int, upscale bool) Plan {
	return p.FillAt(width, height, upscale, Center)
}

// Crop returns a plan cropping the specified rectangle in pixel coordinates.
func (p Plan) Crop(x, y, width, height int) Plan {
	return p.append(step{kind: crop, x: x, y: y, width: width, height: height})
}

// Blur returns a plan with a Gaussian blur using sigma.
func (p Plan) Blur(sigma float64) Plan { return p.append(step{kind: blur, number: sigma}) }

// Grayscale returns a plan converting pixels to grayscale.
func (p Plan) Grayscale() Plan { return p.append(step{kind: grayscale}) }

// Rotate returns a plan rotating by the selected quarter-turn angle.
func (p Plan) Rotate(r Rotation) Plan { return p.append(step{kind: rotate, number: float64(r)}) }

// FlipHorizontal returns a plan mirroring pixels horizontally.
func (p Plan) FlipHorizontal() Plan { return p.append(step{kind: flipHorizontal}) }

// FlipVertical returns a plan mirroring pixels vertically.
func (p Plan) FlipVertical() Plan { return p.append(step{kind: flipVertical}) }

// Brightness adds an 8-bit channel offset, preserving alpha.
func (p Plan) Brightness(offset int) Plan {
	return p.append(step{kind: brightness, number: float64(offset)})
}

// Contrast applies the reference framework's squared (100+value)/100 factor
// around the channel midpoint. Values outside [-100,100] are rejected.
func (p Plan) Contrast(value float64) Plan { return p.append(step{kind: contrast, number: value}) }

// Validate rejects invalid settings and transforms with an invalid fault; it does not
// inspect input pixels.
func (p Plan) Validate() error {
	if err := p.nativeEncoding.validate(p.output); err != nil {
		return err
	}
	if err := p.encoding.validate(p.output); err != nil {
		return err
	}
	if len(p.steps) > MaxTransforms || p.quality < 0 || p.quality > 100 || p.avifQuality < 0 || p.avifQuality > 100 || p.orientation > IgnoreOrientation || p.frames > PreserveAnimation || p.metadata > PreserveColorProfile || p.resampling > NearestNeighbor {
		return invalid("invalid image plan")
	}
	if p.avifQuality != 0 && p.output != AVIF {
		return invalid("AVIF quality requires explicit AVIF output")
	}
	if p.flatten && p.background.A != 255 {
		return invalid("image background must be opaque")
	}
	if p.output != "" {
		if err := p.output.Validate(); err != nil {
			return err
		}
		if p.quality != 0 && p.output != JPEG {
			return invalid("JPEG quality applies only to JPEG")
		}
	}
	for _, s := range p.steps {
		if err := s.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s step) validate() error {
	if math.IsNaN(s.number) || math.IsInf(s.number, 0) ||
		math.IsNaN(s.amount) || math.IsInf(s.amount, 0) ||
		math.IsNaN(s.threshold) || math.IsInf(s.threshold, 0) || s.position > BottomRight || s.interest > CropEntropy {
		return invalid("invalid image transform")
	}
	switch s.kind {
	case resizeExact, resizeDown, resizeFit, resizeFill, resizeSmart, resizePad, resizeCanvas, crop, cropPositioned:
		if s.width < 1 || s.height < 1 || s.width > 65535 || s.height > 65535 || s.x < 0 || s.y < 0 || s.x > 65535 || s.y > 65535 {
			return invalid("invalid image dimensions")
		}
	case resizeWidth, resizeHeight:
		if s.width < 1 || s.width > 65535 {
			return invalid("invalid image dimension")
		}
	case resizeCanvasRelative:
		if s.width < -65535 || s.width > 65535 || s.height < -65535 || s.height > 65535 {
			return invalid("invalid relative canvas dimensions")
		}
	case blur, sharpen:
		if s.number <= 0 || s.number > 100 || s.amount < 0 || s.amount > 10 || s.threshold < 0 || s.threshold > 1 {
			return invalid("invalid image blur or sharpen")
		}
	case rotate:
		if s.number != 90 && s.number != 180 && s.number != 270 {
			return invalid("invalid image rotation")
		}
	case rotateDegrees:
		if s.number < -360 || s.number > 360 {
			return invalid("image rotation must be between -360 and 360 degrees")
		}
	case brightness:
		if s.number < -255 || s.number > 255 {
			return invalid("invalid image brightness")
		}
	case contrast, saturation:
		if s.number < -100 || s.number > 100 {
			return invalid("image adjustment must be between -100 and 100")
		}
	case gamma:
		if s.number < 0.01 || s.number > 100 {
			return invalid("image gamma must be between 0.01 and 100")
		}
	case hue:
		if s.number < -360 || s.number > 360 {
			return invalid("image hue must be between -360 and 360")
		}
	case sepia, threshold:
		if s.number < 0 || s.number > 100 {
			return invalid("image adjustment must be between 0 and 100")
		}
	case pixelate:
		if s.width < 1 || s.width > 65535 {
			return invalid("invalid pixel block size")
		}
	case insert, mask:
		if len(s.layer.data) == 0 || s.maskMode > LuminanceMask {
			return invalid("image insertion or mask requires a nonempty image result")
		}
		if err := s.placement.Validate(); err != nil {
			return err
		}
		if s.kind == mask && s.placement.Blend != BlendNormal {
			return invalid("image masks do not accept a blend mode")
		}
	case grayscale, flipHorizontal, flipVertical, invert:
	case drawPath:
		if err := s.path.Validate(); err != nil {
			return err
		}
		return s.style.Validate()
	case drawText:
		return s.text.validate()
	default:
		return invalid("invalid image transform")
	}
	return nil
}

// scratch is this transform's intermediate working bytes per peak-canvas
// pixel beyond its input and output images.
func (s step) scratch() int64 {
	switch s.kind {
	case resizeExact, resizeDown, resizeFit, resizeWidth, resizeHeight, blur:
		return scratchBytes
	case resizeSmart:
		return scratchBytes + transformBytes + 64
	case resizeFill, resizePad:
		return scratchBytes + transformBytes
	case sharpen:
		return 2 * scratchBytes
	case drawPath:
		return 10 // Fill and stroke coverage masks plus vector buffers.
	case drawText:
		return 5 // One alpha mask and one vector buffer.
	default:
		return 0
	}
}

// encodingFormat resolves the zero-plan default in one place for both input
// processing and blank-canvas creation.
func (p Plan) encodingFormat(fallback Format) (Format, error) {
	format := p.output
	if format == "" {
		format = fallback
	}
	if p.quality != 0 && format != JPEG {
		return "", invalid("JPEG quality applies only to JPEG")
	}
	return format, nil
}
