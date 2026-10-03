package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func testFont(t *testing.T) Font {
	t.Helper()
	f, err := BuiltinFont(FontRegular)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFontOwnershipRedactionAndBuiltins(t *testing.T) {
	data := bytes.Clone(goregular.TTF)
	f, err := ParseFont(data)
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	e := testEngine(t, DefaultConfig())
	options := TextOptions{Font: f, Size: 20, Color: color.NRGBA{A: 255}}
	result, err := e.Create(t.Context(), 100, 50, color.NRGBA{}, NewPlan().Text("Foundry", options))
	if err != nil || result.Size() == 0 {
		t.Fatal("font retained caller bytes", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%x"} {
		if got := fmt.Sprintf(format, f); got != "image font" {
			t.Fatalf("font formatting exposed data: %q", got)
		}
		if got := fmt.Sprintf(format, NewPlan().Text("private label", options)); strings.Contains(got, "private") {
			t.Fatal("plan exposed text")
		}
	}
	for style := FontRegular; style <= FontMono; style++ {
		if _, err := BuiltinFont(style); err != nil {
			t.Fatal(style, err)
		}
	}
	if _, err := BuiltinFont(FontStyle(255)); err == nil {
		t.Fatal("invalid builtin accepted")
	}
	for _, data := range [][]byte{nil, []byte("not a font"), make([]byte, MaxFontBytes+1)} {
		if _, err := ParseFont(data); err == nil {
			t.Fatal("invalid font accepted")
		}
	}
}

func TestTextPositionWrappingAlignmentAndPixels(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	o := TextOptions{Font: testFont(t), Size: 20, Color: color.NRGBA{R: 255, A: 128}, Position: TopLeft}
	base, err := e.Create(t.Context(), 160, 100, color.NRGBA{}, NewPlan().Text("MO", o))
	if err != nil {
		t.Fatal(err)
	}
	baseImage := resultImage(t, base)
	baseBounds := alphaBounds(baseImage)
	if baseBounds.Empty() {
		t.Fatal("text rendered no pixels")
	}
	antialias := false
	for y := 0; y < 100; y++ {
		for x := 0; x < 160; x++ {
			c := color.NRGBAModel.Convert(baseImage.At(x, y)).(color.NRGBA)
			if c.A > 0 && (c.R != 255 || c.G != 0 || c.B != 0 || c.A > 128) {
				t.Fatal("incorrect text color/alpha", c)
			}
			antialias = antialias || (c.A > 0 && c.A < 128)
		}
	}
	if !antialias {
		t.Fatal("text is not antialiased")
	}
	layout, err := layoutText(t.Context(), &textSpec{text: "MO", options: o}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for position := Center; position <= BottomRight; position++ {
		opts := o
		opts.Position = position
		result, err := e.Create(t.Context(), 160, 100, color.NRGBA{}, NewPlan().Text("MO", opts))
		if err != nil {
			t.Fatal(err)
		}
		shift := position.point(image.Rect(0, 0, 160, 100), image.Rect(0, 0, layout.width, layout.height))
		if got := alphaBounds(resultImage(t, result)); got != baseBounds.Add(shift) {
			t.Fatalf("position %d: got %v want %v", position, got, baseBounds.Add(shift))
		}
	}
	pair, err := layoutText(t.Context(), &textSpec{text: "AA", options: o}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	o.MaxWidth = pair.width + 1
	wrapped, err := layoutText(t.Context(), &textSpec{text: "AA BB", options: o}, DefaultLimits())
	if err != nil || len(wrapped.lines) != 2 {
		t.Fatal("word wrap", len(wrapped.lines), err)
	}
	long, err := layoutText(t.Context(), &textSpec{text: "AAAA", options: o}, DefaultLimits())
	if err != nil || len(long.lines) != 2 {
		t.Fatal("long word wrap", len(long.lines), err)
	}
	o.MaxWidth = 100
	for _, alignment := range []TextAlignment{AlignLeft, AlignCenter, AlignRight} {
		opts := o
		opts.Align = alignment
		result, err := e.Create(t.Context(), 160, 100, color.NRGBA{}, NewPlan().Text("MO", opts))
		if err != nil {
			t.Fatal(err)
		}
		got := alphaBounds(resultImage(t, result))
		if alignment == AlignLeft && got != baseBounds {
			t.Fatal("left alignment shifted text")
		}
		if alignment == AlignCenter && (got.Min.X < 30 || got.Min.X > 40) {
			t.Fatal("center alignment", got)
		}
		if alignment == AlignRight && (got.Max.X < 98 || got.Max.X > 101) {
			t.Fatal("right alignment", got)
		}
	}
	newlines, err := layoutText(t.Context(), &textSpec{text: "A\r\n\nB", options: o}, DefaultLimits())
	if err != nil || len(newlines.lines) != 3 {
		t.Fatal("explicit empty line lost", err)
	}
}

func TestTextValidationMissingGlyphsAndLimits(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	o := TextOptions{Font: testFont(t), Size: 20, Color: color.NRGBA{A: 255}}
	for _, text := range []string{"éΩ", "", "\n"} {
		if _, err := e.Create(t.Context(), 100, 100, color.NRGBA{}, NewPlan().Text(text, o)); err != nil {
			t.Fatal(text, err)
		}
	}
	if _, err := e.Create(t.Context(), 100, 100, color.NRGBA{}, NewPlan().Text("\U0001F984", o)); err == nil {
		t.Fatal("missing glyph silently accepted")
	}
	replace := o
	replace.MissingGlyph = ReplaceMissingGlyph
	if _, err := e.Create(t.Context(), 100, 100, color.NRGBA{}, NewPlan().Text("\U0001F984", replace)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string([]byte{0xff}), "a\x00b", strings.Repeat("a", MaxTextRunes+1)} {
		if err := NewPlan().Text(text, o).Validate(); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
	for _, bad := range []TextOptions{{}, {Font: o.Font, Size: math.NaN()}, {Font: o.Font, Size: 513}, {Font: o.Font, Size: 20, LineHeight: math.NaN()}, {Font: o.Font, Size: 20, Align: TextAlignment(9)}} {
		if err := NewPlan().Text("A", bad).Validate(); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	small := o
	small.MaxWidth = 1
	if _, err := e.Create(t.Context(), 100, 100, color.NRGBA{}, NewPlan().Text("W", small)); err == nil {
		t.Fatal("too-small wrap width accepted")
	}
	config := DefaultConfig()
	config.Limits.InputBytes = int64(len(o.Font.data))
	if _, err := testEngine(t, config).Create(t.Context(), 100, 100, color.NRGBA{}, NewPlan().Text("A", o)); err == nil {
		t.Fatal("font/text bytes not admitted")
	}
	config = DefaultConfig()
	config.Limits.OutputBytes = 1000
	config.Limits.WorkingBytes = textWorkspaceBytes - 1
	if _, err := testEngine(t, config).Create(t.Context(), 100, 100, color.NRGBA{}, NewPlan().Text("A", o)); err == nil {
		t.Fatal("text workspace not admitted")
	}
	config = DefaultConfig()
	config.Limits.Height = 50
	if _, err := testEngine(t, config).Create(t.Context(), 100, 40, color.NRGBA{}, NewPlan().Text("A\nB\nC", o)); err == nil {
		t.Fatal("text layout height not admitted")
	}
}

func TestConcurrentTextAndPathPlans(t *testing.T) {
	e := testEngine(t, DefaultConfig())
	plan := NewPlan().Circle(20, 20, 15, ShapeStyle{Fill: color.NRGBA{B: 255, A: 255}}).
		Text("Foundry", TextOptions{Font: testFont(t), Size: 20, Color: color.NRGBA{R: 255, A: 255}})
	baseline, err := e.Create(t.Context(), 120, 60, color.NRGBA{}, plan)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			result, err := e.Create(t.Context(), 120, 60, color.NRGBA{}, plan)
			if err != nil || !bytes.Equal(result.Bytes(), baseline.Bytes()) {
				t.Error("concurrent drawing differed", err)
			}
		})
	}
	wg.Wait()
}
