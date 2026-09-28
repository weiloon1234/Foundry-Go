package http

import (
	"bytes"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func FuzzETagCapturePreservesPrefix(f *testing.F) {
	f.Add([]byte(""), uint16(1), byte(1), false)
	f.Add([]byte("a prefix followed by an overflow"), uint16(4), byte(3), false)
	f.Add([]byte("flush then continue"), uint16(64), byte(2), true)
	f.Fuzz(func(t *testing.T, body []byte, bound uint16, step byte, flush bool) {
		if len(body) > 64<<10 {
			return
		}
		config := DefaultETagConfig()
		config.MaxBytes = int64(1 + bound%32768)
		didFlush := false
		handler := etagTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			for offset := 0; offset < len(body); {
				end := min(offset+1+int(step), len(body))
				if n, err := w.Write(body[offset:end]); err != nil || n != end-offset {
					t.Fatal("capture write changed", n, err)
				}
				offset = end
				if flush && !didFlush {
					didFlush = true
					if err := stdhttp.NewResponseController(w).Flush(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
		expectedTag := int64(len(body)) <= config.MaxBytes && !didFlush
		if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), body) || (response.Header().Get("ETag") != "") != expectedTag {
			t.Fatal("buffer boundaries changed response or validator", len(body), config.MaxBytes, didFlush)
		}
	})
}
