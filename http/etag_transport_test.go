package http

import (
	"bytes"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestETagPreservesSuccessStatusAndEmptyMedia(t *testing.T) {
	for _, status := range []int{200, 201, 202, 203, 207, 208, 226} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "representation")
			})
			first := etagTestRequest(handler, "GET", "", "")
			if first.Code != status || first.Body.String() != "representation" || first.Header().Get("ETag") == "" {
				t.Fatal("success changed", first.Code, first.Header())
			}
			matched := etagTestRequest(handler, "GET", "If-None-Match", first.Header().Get("ETag"))
			if matched.Code != 304 || matched.Body.Len() != 0 {
				t.Fatal("success revalidation failed", matched.Code)
			}
		})
	}
	for _, explicit := range []bool{false, true} {
		handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if explicit {
				w.Header().Set("Content-Type", "application/json")
			}
		})
		response := etagTestRequest(handler, "GET", "", "")
		want := ""
		if explicit {
			want = "application/json"
		}
		if response.Header().Get("Content-Type") != want || response.Body.Len() != 0 || response.Header().Get("ETag") == "" {
			t.Fatal("empty representation media changed", response.Header())
		}
	}
}

func TestETagCachePolicyDoesNotInterpretQuotedExtensions(t *testing.T) {
	for _, tc := range []struct {
		header string
		tag    bool
	}{
		{"private, max-age=0", true},
		{"NO-STORE", false},
		{"no-store=ignored", false},
		{"extension=\"private,no-store,no-transform\"", true},
		{"extension=\"x,\\\"no-store,more\", max-age=0", true},
		{"extension=\"x,more\", no-store", false},
	} {
		handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Cache-Control", tc.header)
			_, _ = io.WriteString(w, "body")
		})
		response := etagTestRequest(handler, "GET", "", "")
		if (response.Header().Get("ETag") != "") != tc.tag {
			t.Fatal("cache extension changed policy", tc.header, response.Header())
		}
	}
}

func TestETagFlushReachesClientBeforeHandlerReturns(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = io.WriteString(w, "first\n")
		if err := stdhttp.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "last\n")
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	defer once.Do(func() { close(release) })
	client := server.Client()
	client.Timeout = 3 * time.Second
	request, _ := stdhttp.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	first := make([]byte, 6)
	if _, err = io.ReadFull(response.Body, first); err != nil || string(first) != "first\n" {
		t.Fatal("prefix held until return", string(first), err)
	}
	if response.Header.Get("ETag") != "" {
		t.Fatal("flushed response was validated")
	}
	once.Do(func() { close(release) })
	tail, err := io.ReadAll(response.Body)
	if err != nil || string(tail) != "last\n" {
		t.Fatal("stream tail lost", string(tail), err)
	}
}

func TestETagFullDuplexPreservesNativeStreamingThreshold(t *testing.T) {
	prefix := strings.Repeat("x", 8192)
	handler := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := stdhttp.NewResponseController(w).EnableFullDuplex(); err != nil {
			t.Error(err)
			return
		}
		if _, err := io.WriteString(w, prefix); err != nil {
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		_, _ = w.Write(body)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	request, _ := stdhttp.NewRequestWithContext(t.Context(), "GET", server.URL, reader)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	first := make([]byte, 512)
	if _, err = io.ReadFull(response.Body, first); err != nil || !bytes.Equal(first, []byte(prefix[:512])) {
		t.Fatal("full-duplex prefix was buffered", err)
	}
	if _, err = io.WriteString(writer, "input"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	tail, err := io.ReadAll(response.Body)
	if err != nil || string(first)+string(tail) != prefix+"input" || response.Header.Get("ETag") != "" {
		t.Fatal("full-duplex exchange changed", err)
	}
}

type etagLimitedTransport struct {
	stdhttp.ResponseWriter
	remaining int
}

func (w *etagLimitedTransport) Write(data []byte) (int, error) {
	if len(data) <= w.remaining {
		w.remaining -= len(data)
		return w.ResponseWriter.Write(data)
	}
	n, err := w.ResponseWriter.Write(data[:w.remaining])
	w.remaining = 0
	if err == nil {
		err = io.ErrClosedPipe
	}
	return n, err
}

func TestETagNativeTransferFailureCannotLookComplete(t *testing.T) {
	payload := strings.Repeat("failure-boundary-", 4096)
	tagged := etagTestHandler(t, DefaultETagConfig(), func(w stdhttp.ResponseWriter, r *stdhttp.Request) { _, _ = io.WriteString(w, payload) })
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		tagged.ServeHTTP(&etagLimitedTransport{ResponseWriter: w, remaining: 8192}, r)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	request, _ := stdhttp.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		return
	} // The failed transfer may abort before headers reach the client.
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err == nil || string(body) == payload {
		t.Fatal("partial native transfer appeared complete", len(body), err)
	}
}

func TestETagFailureReleasesCaptureAndDoesNotReadAgain(t *testing.T) {
	config := DefaultETagConfig()
	config.MaxConcurrent = 1
	handler := etagTestHandler(t, config, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_, _ = io.WriteString(w, "captured")
		if r.URL.Path == "/panic" {
			panic("fixture")
		}
	})
	func() {
		defer func() {
			if recover() != "fixture" {
				t.Error("handler panic changed")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/panic", nil))
	}()
	if response := etagTestRequest(handler, "GET", "", ""); response.Header().Get("ETag") == "" {
		t.Fatal("panic retained capture permit")
	}
	sentinel := errors.New("failed source")
	writer := &etagResponse{native: httptest.NewRecorder(), request: httptest.NewRequest("GET", "/", nil), err: sentinel}
	reader := &etagCountingReader{}
	if _, err := writer.ReadFrom(reader); !errors.Is(err, sentinel) || reader.calls != 0 {
		t.Fatal("failed response performed more source I/O", err)
	}
}

type etagCountingReader struct{ calls int }

func (r *etagCountingReader) Read([]byte) (int, error) { r.calls++; return 0, io.EOF }
