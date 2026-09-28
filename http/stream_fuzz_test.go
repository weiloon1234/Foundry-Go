package http

import (
	"bytes"
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func FuzzStreamLengthAndCleanup(f *testing.F) {
	f.Add([]byte("payload"), int16(7), uint16(10), true, uint8(3))
	f.Add([]byte("too long"), int16(3), uint16(4), true, uint8(2))
	f.Add([]byte{}, int16(0), uint16(1), false, uint8(1))
	f.Fuzz(func(t *testing.T, payload []byte, declared int16, ceiling uint16, known bool, chunk uint8) {
		if len(payload) > 8192 {
			return
		}
		limit := int64(ceiling%8192) + 1
		limits := DefaultEndpointLimits()
		limits.Files.Bytes = limit
		var closes atomic.Int32
		router := streamRouter(t, streamEndpoint().WithLimits(limits), func(context.Context) (StreamContent, error) {
			source := bytes.NewReader(payload)
			c := StreamContent{Body: fileReaderCallbacks{read: func(p []byte) (int, error) { return source.Read(p[:min(len(p), int(chunk)+1)]) }, close: func() error { closes.Add(1); return nil }}, MediaType: "text/plain; charset=utf-8"}
			if known {
				c.Length = value.Set(int64(declared))
			}
			return c, nil
		})
		recorder := httptest.NewRecorder()
		var aborted bool
		func() {
			defer func() {
				if got := recover(); got != nil {
					if got != stdhttp.ErrAbortHandler {
						t.Fatalf("callback escaped: %v", got)
					}
					aborted = true
				}
			}()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/stream", nil))
		}()
		valid := int64(len(payload)) <= limit && (!known || int64(declared) == int64(len(payload)))
		if closes.Load() != 1 {
			t.Fatal("stream leaked", closes.Load())
		}
		if valid {
			if aborted || recorder.Code != 200 || !bytes.Equal(recorder.Body.Bytes(), payload) {
				t.Fatal("valid stream changed", recorder.Code, aborted)
			}
		} else if !aborted && recorder.Code != 500 {
			t.Fatal("invalid length accepted", recorder.Code)
		}
		if aborted && int64(recorder.Body.Len()) > limit {
			t.Fatal("byte bound exceeded")
		}
	})
}
