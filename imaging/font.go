package imaging

import (
	"fmt"
	"slices"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

const MaxFontBytes = 8 << 20

// Font owns an immutable TrueType/OpenType font. Load application font assets
// once and reuse them across plans; mutable font faces never escape the engine.
// Font loading is for application assets, not arbitrary uploaded fonts.
type Font struct {
	data []byte
	font *sfnt.Font
}

// ParseFont copies data before parsing. Collections, WOFF and bitmap/color
// fonts are not supported by this portable outline renderer.
func ParseFont(data []byte) (Font, error) {
	if len(data) == 0 || len(data) > MaxFontBytes {
		return Font{}, invalid("invalid image font size")
	}
	owned := slices.Clone(data)
	f, err := sfnt.Parse(owned)
	if err != nil {
		return Font{}, invalid("invalid image font")
	}
	return Font{data: owned, font: f}, nil
}

func (Font) String() string             { return "image font" }
func (Font) GoString() string           { return "image font" }
func (Font) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("image font")) }

type FontStyle uint8

const (
	FontRegular FontStyle = iota
	FontBold
	FontItalic
	FontBoldItalic
	FontMono
)

// BuiltinFont loads one of the Go fonts already included with Foundry. Apps
// need no font download, filesystem path or additional Go dependency.
func BuiltinFont(style FontStyle) (Font, error) {
	switch style {
	case FontRegular:
		return ParseFont(goregular.TTF)
	case FontBold:
		return ParseFont(gobold.TTF)
	case FontItalic:
		return ParseFont(goitalic.TTF)
	case FontBoldItalic:
		return ParseFont(gobolditalic.TTF)
	case FontMono:
		return ParseFont(gomono.TTF)
	default:
		return Font{}, invalid("invalid built-in image font")
	}
}
