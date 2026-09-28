package http

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"testing"
)

func FuzzMultipartEndpointBoundsAndCleanup(f *testing.F) {
	directory := f.TempDir()
	limits := DefaultEndpointLimits()
	limits.Multipart = MultipartLimits{Bytes: 4096, FileBytes: 1024, Parts: 8, Files: 2, Readers: 2, HeaderBytes: 512, FieldBytes: 256, FieldsBytes: 512, Issues: 4}
	limits.Body.Bytes = 256
	endpoint := multipartEndpoint(directory).WithLimits(limits)
	router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) { return NoContent{}, nil }))
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte("--foundry-fuzz\r\nContent-Disposition: form-data; name=\"primary\"; filename=\"a.txt\"\r\n\r\ncontents\r\n--foundry-fuzz--\r\n"))
	f.Add([]byte("--foundry-fuzz--\r\n"))
	f.Add([]byte("invalid\xff\x00body"))
	f.Fuzz(func(t *testing.T, data []byte) {
		request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(data))
		request.Header.Set("Content-Type", "multipart/form-data; boundary=foundry-fuzz")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		switch response.Code {
		case 204, 400, 413:
		default:
			t.Fatalf("unexpected multipart status %d: %s", response.Code, response.Body)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatalf("multipart retained temporary files: entries=%d error=%v", len(entries), err)
		}
	})
}
