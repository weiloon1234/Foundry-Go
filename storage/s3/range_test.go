package s3

import "testing"

func FuzzContentRange(f *testing.F) {
	for _, seed := range []string{"bytes 0-2/3", "bytes 7-9/10", "bytes */10", "bytes 0-9223372036854775807/9223372036854775807", "invalid"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		offset, length, total, err := parseContentRange(text)
		if err == nil && (offset < 0 || length <= 0 || total <= 0 || offset > total-length) {
			t.Fatal("invalid accepted span")
		}
	})
}
