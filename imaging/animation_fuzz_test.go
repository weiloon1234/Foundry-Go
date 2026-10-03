package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func FuzzAnimationContainers(f *testing.F) {
	input := apngFixture(f, 4, 4, 0, nil, []apngTestFrame{{img: solidImage(4, 4, color.White), delay: frameDelay{1, 100}}, {img: solidImage(2, 2, color.Black), x: 1, y: 1, delay: frameDelay{3, 100}, dispose: 2, blend: 1}})
	f.Add(input)
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Transparent, color.White})
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{1, 2}}); err != nil {
		f.Fatal(err)
	}
	f.Add(encoded.Bytes())
	f.Add(webpInput(f, "lossless"))
	f.Add(webpInput(f, "lossy"))
	config := DefaultConfig()
	config.Limits.Width, config.Limits.Height, config.Limits.Pixels = 128, 128, 16384
	config.Limits.Frames = 4
	config.Limits.InputBytes, config.Limits.OutputBytes = 1<<16, 1<<16
	config.Limits.WorkingBytes = 4 << 20
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		result, err := testEngine(t, config).ProcessBytes(t.Context(), data, NewPlan().Frames(PreserveAnimation).Format(PNG))
		if err != nil {
			if result.Size() != 0 {
				t.Fatal("failed animation returned partial output")
			}
			return
		}
		info, err := Inspect(result.Bytes(), config.Limits)
		if err != nil || info.Images != result.Info().Images || info.Animated != result.Info().Animated {
			t.Fatal("animation metadata differs from output", err)
		}
	})
}
