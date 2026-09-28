package imaging

import "testing"

func FuzzInspect(f *testing.F) {
	for _, seed := range [][]byte{[]byte("GIF89a"), []byte("II\x2a\x00\x08\x00\x00\x00"), []byte("\x89PNG\r\n\x1a\n"), {0, 0, 1, 0, 1, 0}, []byte("RIFF\x04\x00\x00\x00WEBP")} {
		f.Add(seed)
	}
	l := DefaultLimits()
	l.InputBytes = 1 << 16
	l.OutputBytes = 1 << 16
	l.Width = 128
	l.Height = 128
	l.Pixels = 16384
	l.Frames = 4
	l.WorkingBytes = 4 << 20
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		info, err := Inspect(data, l)
		if err == nil && (info.Width < 1 || info.Height < 1 || info.Images < 1 || info.Width > 128 || info.Height > 128) {
			t.Fatal("accepted invalid metadata")
		}
	})
}
