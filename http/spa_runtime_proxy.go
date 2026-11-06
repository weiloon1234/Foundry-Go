package http

import (
	"bytes"
	stdhttp "net/http"
	"net/http/httputil"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// WrapProxy returns a handler using a copy of proxy and the same runtime descriptor
// as SPAConfig.Runtime. The caller retains routing, upstream configuration and
// ownership of the transport; do not mutate proxy or its dependencies afterward.
// navigation selects possible HTML documents. It must be concurrency-safe and
// exclude APIs/assets that need native conditional or range behavior. It runs only
// for GET/HEAD without upgrades. Other requests use the original proxy unchanged.
//
// Selected requests lose conditional, range and encoding headers before forwarding;
// HEAD fetches GET upstream but emits no body. Only successful text/html is buffered
// and injected. Non-HTML bodies stream normally. Existing ModifyResponse runs first;
// existing ErrorHandler handles upstream failures, while injection failures use the
// framework's safe 503 response. Put compression and CSP middleware outside this
// handler. No middleware or idempotent-route metadata is changed.
func (d SPARuntime) WrapProxy(proxy *httputil.ReverseProxy, navigation func(*stdhttp.Request) bool) (stdhttp.Handler, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if proxy == nil || navigation == nil {
		return nil, safeerror.NewFault(fault.Invalid, "SPA runtime proxy requires a proxy and navigation predicate")
	}
	originalProxy := *proxy
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet && r.Method != stdhttp.MethodHead || r.Header.Get("Upgrade") != "" {
			originalProxy.ServeHTTP(w, r)
			return
		}
		selected := false
		err := callback.Isolated("HTTP SPA navigation predicate", func() error { selected = navigation(r); return nil })
		if err != nil {
			spaRuntimeError(w, r, err)
			return
		}
		if !selected {
			originalProxy.ServeHTTP(w, r)
			return
		}
		if r.Method == stdhttp.MethodHead {
			w = spaRuntimeHeadWriter{w}
		}
		var failure error
		wrapped := originalProxy
		wrapped.ModifyResponse = func(response *stdhttp.Response) error {
			if originalProxy.ModifyResponse != nil {
				if err := originalProxy.ModifyResponse(response); err != nil {
					return err
				}
			}
			failure = safeerror.Prefix("SPA runtime proxy document failed", d.state.proxyDocument(response, r))
			return failure
		}
		wrapped.ErrorHandler = func(w stdhttp.ResponseWriter, _ *stdhttp.Request, err error) {
			if failure != nil {
				spaRuntimeError(w, r, failure)
				return
			}
			if originalProxy.ErrorHandler != nil {
				originalProxy.ErrorHandler(w, r, err)
				return
			}
			writeRoutingError(w, r, Unavailable.WithCause(safeerror.Prefix("SPA upstream failed", err)))
		}
		request := r.Clone(r.Context())
		for _, name := range []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "Range", "If-Range"} {
			request.Header.Del(name)
		}
		// An explicit identity value also prevents the default Transport from
		// automatically negotiating gzip after an absent Accept-Encoding.
		request.Header.Set("Accept-Encoding", "identity")
		if request.Method == stdhttp.MethodHead {
			request.Method = stdhttp.MethodGet
		}
		wrapped.ServeHTTP(w, request)
	}), nil
}

func (s *spaRuntimeState) proxyDocument(response *stdhttp.Response, original *stdhttp.Request) error {
	// Even an upstream ignoring stripped request headers must not publish a stale
	// document validator or partial representation as a runtime document.
	isHTML := spaHTML(response.Header.Get("Content-Type"))
	if (response.StatusCode == stdhttp.StatusNotModified || response.StatusCode == stdhttp.StatusPartialContent) && (isHTML || response.Header.Get("Content-Type") == "") {
		return safeerror.NewFault(fault.Invalid, "SPA upstream returned a conditional or partial document")
	}
	if response.StatusCode == stdhttp.StatusOK && isHTML {
		if response.Header.Get("Content-Encoding") != "" || hasResponseTrailers(response.Header) || len(response.Trailer) != 0 {
			return safeerror.NewFault(fault.Invalid, "SPA upstream HTML requires an unencoded document without trailers")
		}
		if err := s.active.Acquire(original.Context(), 0, nil); err != nil {
			return err
		}
		transferred := false
		defer func() {
			if !transferred {
				s.active.Release()
			}
		}()
		var body []byte
		err := callback.Isolated("HTTP SPA proxy body", func() (err error) {
			// ReverseProxy closes response.Body on failure. On success replace it
			// only after the upstream body is closed, preserving ownership.
			body, err = s.read(response.Body)
			if err != nil {
				return err
			}
			body, err = s.document(original, body)
			return err
		})
		if err != nil {
			return err
		}
		if err := closeAssetFile(response.Body); err != nil {
			response.Body = stdhttp.NoBody
			return err
		}
		response.Body = &spaRuntimeProxyBody{Reader: bytes.NewReader(body), release: s.active.Release}
		transferred = true
		response.ContentLength = int64(len(body))
		spaRuntimeHeaders(response.Header, len(body))
	}
	if original.Method == stdhttp.MethodHead {
		body := response.Body
		response.Body = stdhttp.NoBody
		return closeAssetFile(body)
	}
	return nil
}

// spaRuntimeProxyBody keeps document capacity until ReverseProxy finishes copying
// or aborts the response; a slow client still owns its buffered document.
type spaRuntimeProxyBody struct {
	*bytes.Reader
	release func()
	once    sync.Once
}

// Close releases the document's admission slot exactly once.
func (b *spaRuntimeProxyBody) Close() error { b.once.Do(b.release); return nil }

type spaRuntimeHeadWriter struct{ stdhttp.ResponseWriter }

// Write suppresses bodies from a caller-supplied proxy error handler on HEAD.
func (w spaRuntimeHeadWriter) Write(data []byte) (int, error) { return len(data), nil }

// Unwrap preserves ResponseController access to the underlying writer.
func (w spaRuntimeHeadWriter) Unwrap() stdhttp.ResponseWriter { return w.ResponseWriter }
