package imaging

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const (
	MaxTextRunes     = 4096
	maxGlyphSegments = 65536
	maxTextSegments  = 1 << 20
	// A bounded SFNT outline buffer, layout records, and wrapping scratch.
	textWorkspaceBytes = 8 << 20
)

type TextAlignment uint8

const (
	AlignLeft TextAlignment = iota
	AlignCenter
	AlignRight
)

type MissingGlyph uint8

const (
	RejectMissingGlyph MissingGlyph = iota
	ReplaceMissingGlyph
)

// TextOptions positions a block of line boxes, with the first baseline one
// font ascent below its top. Size is pixels per em (1..512). MaxWidth wraps at
// spaces, splitting long words when necessary; zero disables wrapping. Explicit
// newlines are preserved, CRLF is normalized, and tabs become four spaces.
// LineHeight is a size multiplier (0.5..4); zero selects 1.2. Align applies
// within MaxWidth, or within the longest line when MaxWidth is zero.
type TextOptions struct {
	Font         Font
	Size         float64
	Color        color.NRGBA
	Position     Position
	X, Y         int
	MaxWidth     int
	Align        TextAlignment
	LineHeight   float64
	MissingGlyph MissingGlyph
}

func (o TextOptions) Validate() error {
	if o.Font.font == nil || !finiteRange(o.Size, 1, 512) || o.Position > BottomRight ||
		o.X < -65535 || o.X > 65535 || o.Y < -65535 || o.Y > 65535 ||
		o.MaxWidth < 0 || o.MaxWidth > 65535 || o.Align > AlignRight || o.MissingGlyph > ReplaceMissingGlyph ||
		(o.LineHeight != 0 && !finiteRange(o.LineHeight, 0.5, 4)) {
		return invalid("invalid image text options")
	}
	return nil
}

type textSpec struct {
	text    string
	options TextOptions
}

// Text draws antialiased outline glyphs with kerning. Font assets and the text
// are owned by the immutable plan. This renderer lays out left-to-right glyphs;
// it does not perform bidirectional layout, script shaping or color emoji.
func (p Plan) Text(text string, options TextOptions) Plan {
	return p.append(step{kind: drawText, text: &textSpec{text: strings.Clone(text), options: options}})
}

func (t *textSpec) validate() error {
	if t == nil || len(t.text) > 4*MaxTextRunes || !utf8.ValidString(t.text) || utf8.RuneCountInString(t.text) > MaxTextRunes {
		return invalid("invalid image text length or encoding")
	}
	if err := t.options.Validate(); err != nil {
		return err
	}
	for _, r := range t.text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return invalid("unsupported image text control character")
		}
	}
	return nil
}

type textGlyph struct {
	index          sfnt.GlyphIndex
	advance, kern  fixed.Int26_6
	space, newline bool
}

type textLine struct {
	start, end int
	width      fixed.Int26_6
}

type textLayout struct {
	glyphs                 []textGlyph
	lines                  []textLine
	width, height          int
	ascent, leading, scale fixed.Int26_6
}

func layoutText(ctx context.Context, spec *textSpec, l Limits) (textLayout, error) {
	o := spec.options
	f := o.Font.font
	scale := fixed.Int26_6(math.Round(o.Size * 64))
	var buf sfnt.Buffer
	metrics, err := f.Metrics(&buf, scale, font.HintingNone)
	if err != nil || metrics.Ascent < 0 || metrics.Descent < 0 {
		return textLayout{}, invalid("invalid image font metrics")
	}
	lineHeight := o.LineHeight
	if lineHeight == 0 {
		lineHeight = 1.2
	}
	layout := textLayout{ascent: metrics.Ascent, leading: fixed.Int26_6(math.Round(o.Size * lineHeight * 64)), scale: scale}
	text := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(spec.text, "\r\n", "\n"), "\r", "\n"), "\t", "    ")
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return textLayout{}, limited()
	}
	var previous sfnt.GlyphIndex
	havePrevious := false
	for _, r := range text {
		if err := ctx.Err(); err != nil {
			return textLayout{}, err
		}
		if r == '\n' {
			layout.glyphs = append(layout.glyphs, textGlyph{newline: true})
			havePrevious = false
			continue
		}
		index, err := f.GlyphIndex(&buf, r)
		if err != nil || (index == 0 && o.MissingGlyph == RejectMissingGlyph) {
			return textLayout{}, invalid("image font does not support a requested glyph")
		}
		advance, err := f.GlyphAdvance(&buf, index, scale, font.HintingNone)
		if err != nil || advance < 0 || advance > fixed.I(65535) {
			return textLayout{}, invalid("invalid image glyph advance")
		}
		var kern fixed.Int26_6
		if havePrevious {
			kern, err = f.Kern(&buf, previous, index, scale, font.HintingNone)
			if err == sfnt.ErrNotFound {
				kern, err = 0, nil
			}
			if err != nil || kern < -fixed.I(65535) || kern > fixed.I(65535) {
				return textLayout{}, invalid("invalid image font kerning")
			}
		}
		layout.glyphs = append(layout.glyphs, textGlyph{index: index, advance: advance, kern: kern, space: r == ' '})
		previous, havePrevious = index, true
	}
	for start := 0; ; {
		end := start
		for end < len(layout.glyphs) && !layout.glyphs[end].newline {
			end++
		}
		lines, err := wrapText(layout.glyphs, start, end, o.MaxWidth)
		if err != nil {
			return textLayout{}, err
		}
		layout.lines = append(layout.lines, lines...)
		if end == len(layout.glyphs) {
			break
		}
		start = end + 1
	}
	for _, line := range layout.lines {
		layout.width = max(layout.width, line.width.Ceil())
	}
	if o.MaxWidth > 0 {
		layout.width = o.MaxWidth
	}
	height := int64(metrics.Ascent) + int64(metrics.Descent) + int64(len(layout.lines)-1)*int64(layout.leading)
	if height > 65535*64 {
		return textLayout{}, limited()
	}
	layout.width, layout.height = max(1, layout.width), max(1, int((height+63)/64))
	if err := l.dimensions(layout.width, layout.height); err != nil {
		return textLayout{}, err
	}
	return layout, nil
}

func wrapText(glyphs []textGlyph, start, end, width int) ([]textLine, error) {
	if start == end {
		return []textLine{{start: start, end: end}}, nil
	}
	var lines []textLine
	for start < end {
		lastSpace := -1
		var x, spaceWidth fixed.Int26_6
		i := start
		for ; i < end; i++ {
			g := glyphs[i]
			next := int64(x) + int64(g.advance)
			if i > start {
				next += int64(g.kern)
			}
			if next < 0 || next > 65535*64 {
				return nil, limited()
			}
			if width > 0 && next > int64(width)*64 {
				if i == start {
					return nil, invalid("image text width is smaller than a glyph")
				}
				if lastSpace >= start {
					lines = append(lines, textLine{start, lastSpace, spaceWidth})
					start = lastSpace + 1
				} else {
					lines = append(lines, textLine{start, i, x})
					start = i
				}
				for start < end && glyphs[start].space {
					start++
				}
				break
			}
			if g.space && i > start {
				lastSpace, spaceWidth = i, x
			}
			x = fixed.Int26_6(next)
		}
		if i == end {
			lines = append(lines, textLine{start, end, x})
			break
		}
	}
	return lines, nil
}

// visitGlyphs is shared by admission and rendering. Outline complexity and
// coordinates are checked even for transparent/off-canvas text.
func visitGlyphs(ctx context.Context, spec *textSpec, layout textLayout, origin image.Point, z *vector.Rasterizer) error {
	var buf sfnt.Buffer
	segmentsSeen := 0
	for n, line := range layout.lines {
		x := fixed.I(origin.X)
		delta := fixed.I(layout.width) - line.width
		switch spec.options.Align {
		case AlignCenter:
			x += delta / 2
		case AlignRight:
			x += delta
		}
		y := fixed.I(origin.Y) + layout.ascent + fixed.Int26_6(n)*layout.leading
		for i := line.start; i < line.end; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			g := layout.glyphs[i]
			if i > line.start {
				x += g.kern
			}
			segments, err := spec.options.Font.font.LoadGlyph(&buf, g.index, layout.scale, nil)
			if err != nil {
				return invalid("image font glyph cannot be rendered")
			}
			segmentsSeen += len(segments)
			if len(segments) > maxGlyphSegments || segmentsSeen > maxTextSegments {
				return limited()
			}
			for _, seg := range segments {
				for _, arg := range seg.Args {
					if arg.X < -fixed.I(65535) || arg.X > fixed.I(65535) || arg.Y < -fixed.I(65535) || arg.Y > fixed.I(65535) {
						return limited()
					}
				}
				if z == nil {
					continue
				}
				a, b, c := seg.Args[0], seg.Args[1], seg.Args[2]
				switch seg.Op {
				case sfnt.SegmentOpMoveTo:
					z.ClosePath()
					z.MoveTo(float32(a.X+x)/64, float32(a.Y+y)/64)
				case sfnt.SegmentOpLineTo:
					z.LineTo(float32(a.X+x)/64, float32(a.Y+y)/64)
				case sfnt.SegmentOpQuadTo:
					z.QuadTo(float32(a.X+x)/64, float32(a.Y+y)/64, float32(b.X+x)/64, float32(b.Y+y)/64)
				case sfnt.SegmentOpCubeTo:
					z.CubeTo(float32(a.X+x)/64, float32(a.Y+y)/64, float32(b.X+x)/64, float32(b.Y+y)/64, float32(c.X+x)/64, float32(c.Y+y)/64)
				default:
					return invalid("invalid image glyph outline")
				}
			}
			if z != nil {
				z.ClosePath()
			}
			x += g.advance
		}
	}
	return nil
}

func drawLabel(ctx context.Context, img image.Image, s step, l Limits) (image.Image, error) {
	layout, err := layoutText(ctx, s.text, l)
	if err != nil {
		return nil, err
	}
	out := ownedNRGBA(img)
	o := s.text.options
	origin := o.Position.point(out.Bounds(), image.Rect(0, 0, layout.width, layout.height)).Add(image.Pt(o.X, o.Y))
	z := vector.NewRasterizer(out.Bounds().Dx(), out.Bounds().Dy())
	if err := visitGlyphs(ctx, s.text, layout, origin, z); err != nil {
		return nil, err
	}
	if err := paintRaster(ctx, out, z, o.Color); err != nil {
		return nil, err
	}
	return out, nil
}
