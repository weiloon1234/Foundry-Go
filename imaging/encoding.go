package imaging

import (
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/vpx/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

	"github.com/weiloon1234/Foundry-Go/value"
)

// PNGCompression selects a lossless PNG/APNG compression effort. Compression
// changes encoding time and file size, never the represented pixel values.
type PNGCompression uint8

const (
	PNGDefaultCompression PNGCompression = iota
	PNGNoCompression
	PNGBestSpeed
	PNGBestCompression
)

// DefaultAVIFSpeed favors fast encoding. Zero, when explicitly selected with
// AVIFSpeed, requests the most thorough search; it is not an omitted setting.
const DefaultAVIFSpeed = 10

// WebPMode selects exact lossless pixels or lossy 4:2:0 color compression.
// Alpha remains lossless in both modes.
type WebPMode uint8

const (
	WebPLossless WebPMode = iota
	WebPLossy
)

const DefaultWebPQuality = 75
const DefaultWebPMethod = 4

type encodingOptions struct {
	pngCompression   value.Optional[PNGCompression]
	avifSpeed        value.Optional[int]
	avifAlphaQuality value.Optional[int]
	webpMode         value.Optional[WebPMode]
	webpQuality      value.Optional[int]
	webpMethod       value.Optional[int]
}

// WebPMode selects WebPLossless (the default) or WebPLossy. Requires explicit
// WebP output. Lossless preserves the RGB of fully transparent pixels too.
func (p Plan) WebPMode(mode WebPMode) Plan {
	p.encoding.webpMode = value.Set(mode)
	return p
}

// WebPQuality selects lossy color quality 1..100, default DefaultWebPQuality.
// Requires explicit WebP output and WebPLossy mode; 100 is still lossy.
func (p Plan) WebPQuality(quality int) Plan {
	p.encoding.webpQuality = value.Set(quality)
	return p
}

// WebPMethod selects search effort 0..6 (fastest to most thorough), default
// DefaultWebPMethod. Explicit zero is preserved. Applies to either WebP mode.
func (p Plan) WebPMethod(method int) Plan {
	p.encoding.webpMethod = value.Set(method)
	return p
}

// PNGCompression selects compression for every encoded PNG/APNG frame. It
// requires an explicit PNG output, including PNGDefaultCompression.
func (p Plan) PNGCompression(compression PNGCompression) Plan {
	p.encoding.pngCompression = value.Set(compression)
	return p
}

// AVIFSpeed selects encoding speed 0..10, from most thorough to fastest search.
// The default is DefaultAVIFSpeed. Requires an explicit AVIF output. A slow
// synchronous encode retains engine capacity until it actually exits.
func (p Plan) AVIFSpeed(speed int) Plan {
	p.encoding.avifSpeed = value.Set(speed)
	return p
}

// AVIFAlphaQuality sets alpha quality independently to 1..100; by default it
// follows AVIFQuality. Requires an explicit AVIF output.
func (p Plan) AVIFAlphaQuality(quality int) Plan {
	p.encoding.avifAlphaQuality = value.Set(quality)
	return p
}

func (o encodingOptions) validate(format Format) error {
	if mode, set := o.webpMode.Get(); set && (format != WebP || mode > WebPLossy) {
		return invalid("WebP mode requires explicit WebP output and a valid mode")
	}
	if quality, set := o.webpQuality.Get(); set && (format != WebP || !o.webpLossy() || quality < 1 || quality > 100) {
		return invalid("WebP quality requires explicit lossy WebP output and a value from 1 to 100")
	}
	if method, set := o.webpMethod.Get(); set && (format != WebP || method < 0 || method > 6) {
		return invalid("WebP method requires explicit WebP output and a value from 0 to 6")
	}
	if compression, set := o.pngCompression.Get(); set && (format != PNG || compression > PNGBestCompression) {
		return invalid("PNG compression requires explicit PNG output and a valid compression level")
	}
	if speed, set := o.avifSpeed.Get(); set && (format != AVIF || speed < 0 || speed > 10) {
		return invalid("AVIF speed requires explicit AVIF output and a value from 0 to 10")
	}
	if quality, set := o.avifAlphaQuality.Get(); set && (format != AVIF || quality < 1 || quality > 100) {
		return invalid("AVIF alpha quality requires explicit AVIF output and a value from 1 to 100")
	}
	return nil
}

func (o encodingOptions) webpLossy() bool {
	mode, _ := o.webpMode.Get()
	return mode == WebPLossy
}

func (o encodingOptions) encodeWebP(w io.Writer, img image.Image) error {
	quality, set := o.webpQuality.Get()
	if !set {
		quality = DefaultWebPQuality
	}
	method, set := o.webpMethod.Get()
	if !set {
		method = DefaultWebPMethod
	}
	// Go JPEG uses full-range YCbCr. The codec's plane fast path expects
	// video-range samples, so use its RGB conversion for these source types.
	if o.webpLossy() {
		switch img.(type) {
		case *image.YCbCr, *image.NYCbCrA:
			img = ownedNRGBA(img)
		}
	}
	output := newWebPOutput(w)
	return webp.Encode(output, img, webp.EncodeOptions{
		Lossless: !o.webpLossy(), Quality: quality, Method: method, Exact: true, Threads: 1,
	})
}

func (o encodingOptions) speed() int {
	if speed, set := o.avifSpeed.Get(); set {
		return speed
	}
	return DefaultAVIFSpeed
}

func (o encodingOptions) alphaQuality() int {
	quality, _ := o.avifAlphaQuality.Get()
	return quality // Codec zero follows the color quality.
}

func (o encodingOptions) encodePNG(w io.Writer, img image.Image) error {
	compression, _ := o.pngCompression.Get()
	level := png.DefaultCompression
	switch compression {
	case PNGNoCompression:
		level = png.NoCompression
	case PNGBestSpeed:
		level = png.BestSpeed
	case PNGBestCompression:
		level = png.BestCompression
	}
	encoder := png.Encoder{CompressionLevel: level}
	return encoder.Encode(w, img)
}

func (p Plan) encode(w io.Writer, img image.Image, f Format, maximum int64) error {
	quality, avifQuality := p.quality, p.avifQuality
	switch f {
	case JPEG:
		if quality == 0 {
			quality = 85
		}
		return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	case PNG:
		return p.encoding.encodePNG(w, img)
	case GIF:
		return gif.Encode(w, img, &gif.Options{NumColors: 256})
	case BMP:
		return bmp.Encode(w, img)
	case TIFF:
		return tiff.Encode(w, img, &tiff.Options{Compression: tiff.Deflate, Predictor: true})
	case WebP:
		return p.encoding.encodeWebP(w, img)
	case AVIF:
		if avifQuality == 0 {
			avifQuality = DefaultAVIFQuality
		}
		return avif.Encode(w, img, avif.EncodeOptions{Quality: avifQuality, Speed: p.encoding.speed(), QualityAlpha: p.encoding.alphaQuality()})
	case ICO:
		return encodeIcon(w, img, maximum)
	default:
		return unsupported()
	}
}

// DefaultAVIFQuality is used unless a plan selects AVIFQuality.
const DefaultAVIFQuality = 60
