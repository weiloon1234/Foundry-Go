package http

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// The response reader must close before input cleanup removes the request's
// temporary file. This exercises both sides of the shared endpoint lifecycle.
func TestDownloadCanConsumeRequestOwnedUploadUntilResponseFinishes(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "short-writer"}[short], func(t *testing.T) {
			directory := t.TempDir()
			endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "uploads.preview", Method: POST, Access: Public}, StaticPath("/preview")), EmptyQuery(), MultipartBody(multipartForm(directory)), DownloadResponse("text/plain; charset=utf-8"))
			var captured UploadedFile
			router, err := NewRouter(endpoint.Handle(func(_ context.Context, input multipartRequestInput) (Download, error) {
				captured = input.Body.Primary
				return DownloadFrom(func(ctx context.Context) (DownloadContent, error) {
					reader, err := captured.Open(ctx)
					return DownloadContent{Body: reader, Name: captured.Name(), MediaType: MediaType(captured.ContentType())}, err
				}), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, multipartTestPart{name: "primary", filename: "preview.txt", media: "text/plain", contents: "file contents", file: true})
			request := httptest.NewRequest("POST", "/preview", bytes.NewReader(wire))
			request.Header.Set("Content-Type", media)
			defer func() {
				recovered := recover()
				if short && recovered != stdhttp.ErrAbortHandler || !short && recovered != nil {
					t.Errorf("unexpected transport outcome: %v", recovered)
				}
				if _, err := captured.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
					t.Error("uploaded file survived response completion", err)
				}
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 0 {
					t.Error("response cleanup retained upload files", err, len(entries))
				}
			}()
			if short {
				router.ServeHTTP(&endpointShortWriter{header: make(stdhttp.Header)}, request)
				return
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != 200 || recorder.Body.String() != "file contents" {
				t.Fatal("upload closed before response read", recorder.Code, recorder.Body.String())
			}
		})
	}
}
