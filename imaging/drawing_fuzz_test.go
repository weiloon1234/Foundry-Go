package imaging

import (
	"image/color"
	"testing"
)

func FuzzTextAndPathPlans(f *testing.F) {
	f.Add("Foundry", float64(12), int16(0), int16(0), uint8(0))
	f.Add("A\nB", float64(20), int16(-10), int16(20), uint8(8))
	font, err := BuiltinFont(FontRegular)
	if err != nil {
		f.Fatal(err)
	}
	config := DefaultConfig()
	config.Limits.Width, config.Limits.Height, config.Limits.Pixels = 256, 256, 65536
	config.Limits.InputBytes, config.Limits.OutputBytes = 1<<20, 1<<20
	config.Limits.WorkingBytes = 16 << 20
	f.Fuzz(func(t *testing.T, text string, size float64, x, y int16, anchor uint8) {
		if len(text) > 4*MaxTextRunes {
			return
		}
		e := testEngine(t, config)
		plan := NewPlan().Circle(float64(x), float64(y), 8, ShapeStyle{Fill: color.NRGBA{B: 255, A: 255}}).
			Text(text, TextOptions{Font: font, Size: size, Position: Position(anchor), MaxWidth: 64, Color: color.NRGBA{R: 255, A: 255}})
		result, err := e.Create(t.Context(), 64, 64, color.NRGBA{}, plan)
		if err != nil {
			if result.Size() != 0 {
				t.Fatal("failed drawing returned a partial result")
			}
			return
		}
		info, err := Inspect(result.Bytes(), config.Limits)
		if err != nil || info.Width != 64 || info.Height != 64 {
			t.Fatal("drawing changed dimensions", err)
		}
	})
}
