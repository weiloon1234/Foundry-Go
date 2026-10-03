package imaging

import (
	"image/color"
	"testing"
)

func FuzzGeometryPreflight(f *testing.F) {
	f.Add(uint8(8), uint8(4), uint8(16), uint8(16), uint8(0), float64(45))
	f.Add(uint8(1), uint8(64), uint8(64), uint8(1), uint8(8), float64(-90))
	config := DefaultConfig()
	config.Limits.Width, config.Limits.Height, config.Limits.Pixels = 128, 128, 16384
	config.Limits.InputBytes, config.Limits.OutputBytes = 1<<16, 1<<16
	config.Limits.WorkingBytes = 4 << 20
	f.Fuzz(func(t *testing.T, width, height, targetWidth, targetHeight, anchor uint8, angle float64) {
		e := testEngine(t, config)
		plan := NewPlan().Contain(int(targetWidth), int(targetHeight), color.NRGBA{}, Position(anchor)).RotateDegrees(angle, color.NRGBA{})
		result, err := e.Create(t.Context(), int(width), int(height), color.NRGBA{R: 255, A: 255}, plan)
		if err != nil {
			if result.Size() != 0 {
				t.Fatal("failed geometry published a partial result")
			}
			return
		}
		info, err := Inspect(result.Bytes(), config.Limits)
		if err != nil || info.Width != result.Info().Width || info.Height != result.Info().Height {
			t.Fatal("geometry preflight disagrees with encoded output", err)
		}
	})
}
