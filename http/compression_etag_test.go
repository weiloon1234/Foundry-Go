package http

import (
	"crypto/sha256"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAutomaticETagDescribesNegotiatedWireRepresentation(t *testing.T) {
	payload := strings.Repeat("a typed response with repeatable bytes\n", 256)
	application := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method != stdhttp.MethodHead {
			_, _ = io.WriteString(w, payload)
		}
	})
	// Declaration order is request-outside-first, matching Rust Foundry's priorities.
	handler, err := ApplyMiddleware(application, ETags(DefaultETagConfig()), Compression(DefaultCompressionConfig()))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, coding, condition, value string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, "/", nil)
		request.Header.Set("Accept-Encoding", coding)
		if condition != "" {
			request.Header.Set(condition, value)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	identity := call("GET", "identity", "", "")
	identityTag := identity.Header().Get("ETag")
	if identity.Code != 200 || identity.Body.String() != payload || identityTag == "" {
		t.Fatal("identity representation changed")
	}
	for _, coding := range []string{"gzip", "br"} {
		t.Run(coding, func(t *testing.T) {
			first := call("GET", coding, "", "")
			tag := first.Header().Get("ETag")
			want := fmt.Sprintf("\"%x\"", sha256.Sum256(first.Body.Bytes()))
			if first.Code != 200 || first.Header().Get("Content-Encoding") != coding || tag != want || tag == identityTag {
				t.Fatal("validator does not describe selected bytes", first.Header(), tag, want)
			}
			if string(decodeCompressed(t, coding, first.Body.Bytes())) != payload {
				t.Fatal("negotiated body changed")
			}
			assertCORSVary(t, first.Header(), "accept-encoding")
			for _, match := range []string{tag, "W/" + tag, "\"unrelated\", " + tag} {
				cached := call("GET", coding, "If-None-Match", match)
				if cached.Code != 304 || cached.Body.Len() != 0 || cached.Header().Get("ETag") != tag {
					t.Fatal("encoded revalidation changed", cached.Code, cached.Header())
				}
				assertCORSVary(t, cached.Header(), "accept-encoding")
			}
			crossed := call("GET", coding, "If-None-Match", identityTag)
			if crossed.Code != 200 || crossed.Header().Get("ETag") != tag {
				t.Fatal("identity validator reused for encoded bytes")
			}
			matched := call("GET", coding, "If-Match", tag)
			if matched.Code != 200 || matched.Header().Get("ETag") != tag {
				t.Fatal("selected strong validator was rejected")
			}
			failed := call("GET", coding, "If-Match", identityTag)
			if failed.Code != 412 || failed.Header().Get("Content-Encoding") != "" || failed.Header().Get("ETag") != "" || !strings.Contains(failed.Body.String(), "precondition_failed") {
				t.Fatal("precondition error retained encoded representation", failed.Code, failed.Header(), failed.Body.String())
			}
			head := call("HEAD", coding, "", "")
			if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("ETag") != "" || head.Header().Get("Content-Encoding") != coding {
				t.Fatal("HEAD invented hashed body metadata", head.Header())
			}
		})
	}
}

func TestCompressionAndETagFullDuplexPreserveNativeStreaming(t *testing.T) {
	for _, etagFirst := range []bool{false, true} {
		t.Run(strconv.FormatBool(etagFirst), func(t *testing.T) {
			prefix := strings.Repeat("x", 8192)
			application := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				if err := stdhttp.NewResponseController(w).EnableFullDuplex(); err != nil {
					t.Error(err)
					return
				}
				if _, err := io.WriteString(w, prefix); err != nil {
					return
				}
				input, err := io.ReadAll(r.Body)
				if err == nil {
					_, _ = w.Write(input)
				}
			})
			middlewares := []Middleware{ETags(DefaultETagConfig()), Compression(DefaultCompressionConfig())}
			if !etagFirst {
				middlewares[0], middlewares[1] = middlewares[1], middlewares[0]
			}
			handler, err := ApplyMiddleware(application, middlewares...)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			client := server.Client()
			client.Timeout = 3 * time.Second
			request, _ := stdhttp.NewRequestWithContext(t.Context(), "GET", server.URL, reader)
			request.Header.Set("Accept-Encoding", "gzip")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			first := make([]byte, 512)
			if _, err = io.ReadFull(response.Body, first); err != nil || string(first) != prefix[:512] {
				t.Fatal("duplex prefix was buffered", err)
			}
			if _, err = io.WriteString(writer, "input"); err != nil {
				t.Fatal(err)
			}
			writer.Close()
			tail, err := io.ReadAll(response.Body)
			if err != nil || string(first)+string(tail) != prefix+"input" || response.Header.Get("ETag") != "" || response.Header.Get("Content-Encoding") != "" {
				t.Fatal("native duplex exchange changed", response.Header, err)
			}
		})
	}
}
