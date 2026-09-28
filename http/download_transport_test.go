package http

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDownloadNativeConnectionRejectsTruncatedTransfer(t *testing.T) {
	declared := strings.NewReader(strings.Repeat("a", 128<<10))
	shorter := strings.NewReader(strings.Repeat("a", 64<<10))
	closed := make(chan struct{})
	router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
		return DownloadFrom(func(context.Context) (DownloadContent, error) {
			return DownloadContent{MediaType: "text/plain; charset=utf-8", Body: fileReaderCallbacks{read: shorter.Read, seek: declared.Seek, close: func() error { close(closed); return nil }}}, nil
		}), nil
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Get(server.URL + "/file")
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("native response failed before transfer", err)
	}
	if err == nil {
		_, readErr := io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if !errors.Is(readErr, io.ErrUnexpectedEOF) {
			t.Fatal("truncated native transfer did not report unexpected EOF", readErr)
		}
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("aborted native transfer leaked its source")
	}
}

func TestDownloadClientCancellationReleasesActiveSource(t *testing.T) {
	entered, closed := make(chan struct{}), make(chan struct{})
	router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
		return DownloadFrom(func(ctx context.Context) (DownloadContent, error) {
			reads := 0
			body := fileReaderCallbacks{
				read: func(p []byte) (int, error) {
					reads++
					if reads == 1 {
						clear(p)
						return len(p), nil
					}
					close(entered)
					<-ctx.Done()
					return 0, ctx.Err()
				},
				seek: func(offset int64, whence int) (int64, error) {
					if whence == io.SeekEnd {
						return 8 << 20, nil
					}
					return offset, nil
				},
				close: func() error { close(closed); return nil },
			}
			return DownloadContent{Body: body, MediaType: "text/plain; charset=utf-8"}, nil
		}), nil
	})
	server := httptest.NewServer(router)
	defer server.Close()
	defer server.CloseClientConnections()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := stdhttp.NewRequestWithContext(ctx, "GET", server.URL+"/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := io.ReadFull(response.Body, make([]byte, 1)); err != nil {
		t.Fatal("response did not stream before full read", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("source did not begin its cancellable read")
	}
	cancel()
	response.Body.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("client cancellation abandoned the source callback")
	}
}

func TestDownloadSourceOwnsResourcesBeforeCallbackReturn(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			released := false
			router := downloadRouter(t, downloadEndpoint(), func(context.Context, downloadRequest) (Download, error) {
				return DownloadFrom(func(context.Context) (DownloadContent, error) {
					// Before return the source, not the framework, owns the local resource.
					defer func() { released = true }()
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic(errors.New("private-opening-data"))
				}), nil
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/file", nil))
			if recorder.Code != 500 || !released || strings.Contains(recorder.Body.String(), "private") {
				t.Fatal("source failure escaped its boundary", recorder.Code)
			}
		})
	}
}
