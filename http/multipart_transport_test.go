package http

import (
	"bytes"
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/testkit"
)

// Native chunked requests prove that file capture does not depend on a known
// Content-Length or a pre-buffered body. A missing closing boundary must discard
// the captured file without entering the domain handler.
func TestMultipartNativeChunkedStreamsAndInterruptedFraming(t *testing.T) {
	const size = 256 << 10
	for _, complete := range []bool{true, false} {
		name := "complete"
		if !complete {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			called := false
			router, err := NewRouter(multipartEndpoint(directory).Handle(func(ctx context.Context, input multipartRequestInput) (NoContent, error) {
				called = true
				reader, err := input.Body.Primary.Open(ctx)
				if err != nil {
					return NoContent{}, err
				}
				defer reader.Close()
				count, err := io.Copy(io.Discard, reader)
				if count != size || input.Body.Primary.Size() != size {
					t.Error("streamed file size changed")
				}
				return NoContent{}, err
			}))
			if err != nil {
				t.Fatal(err)
			}
			finished := make(chan struct{})
			server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				defer close(finished)
				if r.ContentLength != -1 || len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked" {
					t.Error("test did not exercise native chunked transport")
				}
				router.ServeHTTP(w, r)
			}))
			defer server.Close()
			const boundary = "foundry-stream-test"
			prefix := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"primary\"; filename=\"stream.bin\"\r\nContent-Type: application/octet-stream\r\n\r\n"
			suffix := ""
			if complete {
				suffix = "\r\n--" + boundary + "--\r\n"
			}
			source := io.MultiReader(strings.NewReader(prefix), io.LimitReader(multipartZeroSource{}, size), strings.NewReader(suffix))
			request, err := stdhttp.NewRequestWithContext(t.Context(), "POST", server.URL+"/uploads", source)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("response read=%v close=%v", readErr, closeErr)
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("request cleanup did not finish")
			}
			status := 204
			if !complete {
				status = 400
			}
			if response.StatusCode != status || called != complete {
				t.Fatalf("status=%d handler=%v", response.StatusCode, called)
			}
			assertMultipartCleanup(t, directory)
		})
	}
}

type multipartZeroSource struct{}

func (multipartZeroSource) Read(data []byte) (int, error) { clear(data); return len(data), nil }

type multipartCountedBody struct {
	source io.Reader
	reads  int
}

func (b *multipartCountedBody) Read(data []byte) (int, error) { b.reads++; return b.source.Read(data) }
func (*multipartCountedBody) Close() error                    { return nil }

func TestSignedMultipartVerifiesBeforeReadingOrCapturing(t *testing.T) {
	directory := t.TempDir()
	signed := multipartEndpoint(directory).Signed(urlTestSigner(t, testkit.NewClock(urlTestTime)))
	location, err := signed.URL(t.Context(), urlTestOrigin, NoPath{}, NoQuery{}, urlTestTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := signedTestHandler(t, signed.Handle(func(_ context.Context, input multipartRequestInput) (NoContent, error) {
		calls++
		if input.Body.Primary.Size() != 7 {
			t.Error("signed endpoint lost concrete file")
		}
		return NoContent{}, nil
	}))
	wire, media := multipartWire(t, primaryUpload("content"))
	for _, valid := range []bool{false, true} {
		target := location
		if !valid {
			target += "&tampered=1"
		}
		body := &multipartCountedBody{source: bytes.NewReader(wire)}
		request := httptest.NewRequest("POST", target, body)
		request.Header.Set("Content-Type", media)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if valid {
			if response.Code != 204 || calls != 1 || body.reads == 0 {
				t.Fatalf("valid signature response=%d calls=%d reads=%d", response.Code, calls, body.reads)
			}
		} else if response.Code != 403 || calls != 0 || body.reads != 0 {
			t.Fatalf("invalid signature response=%d calls=%d reads=%d", response.Code, calls, body.reads)
		}
		assertMultipartCleanup(t, directory)
	}
}
