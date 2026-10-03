package imaging

import "testing"

func FuzzAVIFContainers(f *testing.F) {
	for _, name := range []string{"yuv420-8", "grid-2x2", "alpha-444", "anim", "clap-irot"} {
		f.Add(avifFixture(f, name))
	}
	limits := DefaultLimits()
	limits.InputBytes = 1 << 16
	limits.WorkingBytes = 16 << 20
	limits.Width, limits.Height, limits.Pixels, limits.Frames = 512, 512, 262144, 8
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		// Call the private parser directly: panic containment must not hide a
		// parser bug. Pixel decode is separately covered by codec/engine tests.
		container, err := inspectAVIF(data, limits)
		if err != nil {
			return
		}
		if container.decodedPixels > limits.Pixels || int64(len(data))+container.workspace() > limits.WorkingBytes {
			t.Fatal("admitted AVIF beyond bounds")
		}
	})
}
