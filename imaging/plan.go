package imaging

import (
	"image"
	"math"
	"slices"
)

type Orientation uint8

const (
	ApplyOrientation Orientation = iota
	IgnoreOrientation
)

type Frames uint8

const (
	RejectAnimation Frames = iota
	FirstFrame
)

// Metadata is deliberately closed. Re-encoding strips EXIF, GPS, XMP, comments
// and ICC data; orientation is applied first by default. No color-profile
// conversion or metadata preservation is implied.
type Metadata uint8

const StripMetadata Metadata = 0

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
)

type step struct {
	kind                stepKind
	width, height, x, y int
	number              float64
	upscale             bool
}

// Plan holds only copied values, not callbacks or mutable image buffers.
// Methods return independent plans; a zero plan has no transforms and keeps the
// detected format. Quality applies only to JPEG, including a detected JPEG.
type Plan struct {
	steps       []step
	output      Format
	quality     int
	orientation Orientation
	frames      Frames
	metadata    Metadata
}

const MaxTransforms = 32

func NewPlan() Plan                           { return Plan{} }
func (p Plan) append(s step) Plan             { p.steps = append(slices.Clone(p.steps), s); return p }
func (p Plan) Format(f Format) Plan           { p.output = f; return p }
func (p Plan) JPEGQuality(q int) Plan         { p.quality = q; return p }
func (p Plan) Orientation(o Orientation) Plan { p.orientation = o; return p }
func (p Plan) Frames(f Frames) Plan           { p.frames = f; return p }
func (p Plan) Metadata(m Metadata) Plan       { p.metadata = m; return p }
func (p Plan) Resize(width, height int) Plan {
	return p.append(step{kind: resizeExact, width: width, height: height, upscale: true})
}
func (p Plan) Fit(width, height int, upscale bool) Plan {
	return p.append(step{kind: resizeFit, width: width, height: height, upscale: upscale})
}
func (p Plan) Fill(width, height int, upscale bool) Plan {
	return p.append(step{kind: resizeFill, width: width, height: height, upscale: upscale})
}
func (p Plan) Crop(x, y, width, height int) Plan {
	return p.append(step{kind: crop, x: x, y: y, width: width, height: height})
}
func (p Plan) Blur(sigma float64) Plan { return p.append(step{kind: blur, number: sigma}) }
func (p Plan) Grayscale() Plan         { return p.append(step{kind: grayscale}) }
func (p Plan) Rotate(r Rotation) Plan  { return p.append(step{kind: rotate, number: float64(r)}) }
func (p Plan) FlipHorizontal() Plan    { return p.append(step{kind: flipHorizontal}) }
func (p Plan) FlipVertical() Plan      { return p.append(step{kind: flipVertical}) }

// Brightness adds an 8-bit channel offset, preserving alpha.
func (p Plan) Brightness(offset int) Plan {
	return p.append(step{kind: brightness, number: float64(offset)})
}

// Contrast applies the reference framework's squared (100+value)/100 factor
// around the channel midpoint. Values outside [-100,100] are rejected.
func (p Plan) Contrast(value float64) Plan { return p.append(step{kind: contrast, number: value}) }

func (p Plan) Validate() error {
	if len(p.steps) > MaxTransforms || p.quality < 0 || p.quality > 100 || p.orientation > IgnoreOrientation || p.frames > FirstFrame || p.metadata != StripMetadata {
		return invalid("invalid image plan")
	}
	if p.output != "" {
		if err := p.output.Validate(); err != nil {
			return err
		}
		if p.quality != 0 && p.output != JPEG {
			return invalid("quality applies only to JPEG; WebP output is lossless")
		}
	}
	for _, s := range p.steps {
		if math.IsNaN(s.number) || math.IsInf(s.number, 0) {
			return invalid("invalid image transform")
		}
		switch s.kind {
		case resizeExact, resizeFit, resizeFill, crop:
			if s.width < 1 || s.height < 1 || s.width > 65535 || s.height > 65535 || s.x < 0 || s.y < 0 || s.x > 65535 || s.y > 65535 {
				return invalid("invalid image dimensions")
			}
		case blur:
			if s.number <= 0 || s.number > 100 {
				return invalid("invalid image blur")
			}
		case rotate:
			if s.number != 90 && s.number != 180 && s.number != 270 {
				return invalid("invalid image rotation")
			}
		case brightness:
			if s.number < -255 || s.number > 255 {
				return invalid("invalid image brightness")
			}
		case contrast:
			if s.number < -100 || s.number > 100 {
				return invalid("invalid image contrast")
			}
		case grayscale, flipHorizontal, flipVertical:
		default:
			return invalid("invalid image transform")
		}
	}
	return nil
}

// dimensions validates intermediate resize canvases as well as final output.
func (s step) dimensions(bounds image.Rectangle, limits Limits) (image.Rectangle, int64, error) {
	w, h := bounds.Dx(), bounds.Dy()
	if err := limits.dimensions(w, h); err != nil {
		return image.Rectangle{}, 0, err
	}
	peak := int64(w) * int64(h)
	switch s.kind {
	case resizeExact:
		peak = max(peak, int64(s.width)*int64(h)) // horizontal resampling scratch
		w, h = s.width, s.height
	case resizeFit, resizeFill:
		scale := math.Min(float64(s.width)/float64(w), float64(s.height)/float64(h))
		if s.kind == resizeFill {
			scale = math.Max(float64(s.width)/float64(w), float64(s.height)/float64(h))
		}
		if !s.upscale && scale > 1 {
			if s.kind == resizeFill {
				return image.Rectangle{}, 0, invalid("fill requires upscaling to reach its declared size")
			}
			scale = 1
		}
		w, h = max(1, int(math.Floor(float64(w)*scale+0.5))), max(1, int(math.Floor(float64(h)*scale+0.5)))
		if err := limits.dimensions(w, h); err != nil {
			return image.Rectangle{}, 0, err
		}
		peak = max(peak, int64(w)*int64(h), int64(w)*int64(bounds.Dy()))
		if s.kind == resizeFill {
			w, h = s.width, s.height
		}
	case crop:
		if s.x > w-s.width || s.y > h-s.height {
			return image.Rectangle{}, 0, invalid("crop is outside the image")
		}
		w, h = s.width, s.height
	case rotate:
		if s.number != 180 {
			w, h = h, w
		}
	}
	if err := limits.dimensions(w, h); err != nil {
		return image.Rectangle{}, 0, err
	}
	return image.Rect(0, 0, w, h), max(peak, int64(w)*int64(h)), nil
}
