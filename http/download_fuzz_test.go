package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func FuzzDownloadRangeBudgetsAndCleanup(f *testing.F) {
	for _, seed := range []string{"", "bytes=0-3", "bytes=0-1,7-9", "bytes=-3", "bytes=999-1000", "bytes=invalid", "bytes=0-0,2-2,4-4,6-6,8-8"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, header string) {
		if len(header) > 4096 {
			return
		}
		var opened, closed atomic.Int32
		endpoint := downloadEndpoint()
		limits := DefaultEndpointLimits()
		limits.Files.Bytes = 64
		limits.Files.Ranges = 4
		limits.Files.RangeBytes = 256
		endpoint = endpoint.WithLimits(limits)
		router := downloadRouter(t, endpoint, func(context.Context, downloadRequest) (Download, error) {
			return DownloadFrom(func(context.Context) (DownloadContent, error) {
				opened.Add(1)
				source := strings.NewReader(strings.Repeat("a", 64))
				return DownloadContent{MediaType: "text/plain; charset=utf-8", Body: fileReaderCallbacks{read: source.Read, seek: source.Seek, close: func() error { closed.Add(1); return nil }}}, nil
			}), nil
		})
		request := httptest.NewRequest("GET", "/file", nil)
		request.Header.Set("Range", header)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if opened.Load() != closed.Load() || opened.Load() > 1 {
			t.Fatal("range request retained source ownership")
		}
		if recorder.Code >= 500 || recorder.Body.Len() > 4096 {
			t.Fatal("range parsing exceeded bounded behavior", recorder.Code, recorder.Body.Len())
		}
	})
}
