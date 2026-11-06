package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"mime"
	stdhttp "net/http"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/admission"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	htmltoken "golang.org/x/net/html"
)

// SPARuntimeConfig bounds public runtime documents. Start with DefaultSPARuntimeConfig.
// HTMLBytes bounds source HTML; JSON.Bytes bounds both encoded and HTML-escaped
// JSON. MaxConcurrent is shared by all uses of one constructed descriptor.
type SPARuntimeConfig struct {
	ElementID     string
	HTMLBytes     int
	JSON          contract.JSONLimits
	MaxConcurrent int
	// StyleNonceCarrier emits an empty style element when CSPNonceFromContext
	// supplies a nonce, for client libraries that discover style nonces in the DOM.
	StyleNonceCarrier bool
}

// DefaultSPARuntimeConfig preserves the portal reader's script ID, with a 2 MiB
// HTML limit, 128 KiB JSON limit and at most 32 concurrent document operations.
func DefaultSPARuntimeConfig() SPARuntimeConfig {
	return SPARuntimeConfig{ElementID: "foundry-portal-runtime", HTMLBytes: 2 << 20,
		JSON: contract.JSONLimits{Bytes: 128 << 10, Depth: 8, Nodes: 4096, Steps: 16384, Issues: 16}, MaxConcurrent: 32}
}

// Validate checks bounded document budgets and a nonempty HTML element ID.
func (c SPARuntimeConfig) Validate() error {
	if c.HTMLBytes < 1 || c.HTMLBytes > 32<<20 || c.JSON.Bytes > 1<<20 || c.MaxConcurrent < 1 || c.MaxConcurrent > 1024 || len(c.ElementID) == 0 || len(c.ElementID) > 256 {
		return safeerror.NewFault(fault.Invalid, "invalid SPA runtime configuration")
	}
	if err := validateParameterText(c.ElementID, false); err != nil {
		return err
	}
	if strings.ContainsAny(c.ElementID, " \t\r\n\f") {
		return safeerror.NewFault(fault.Invalid, "SPA runtime element ID contains whitespace")
	}
	return c.JSON.Validate()
}

// SPARuntime is an immutable typed-loader declaration with shared admission state.
// Construct it with DefineSPARuntime. Copies share capacity; separate constructors
// are independent. Its zero value disables SPAConfig.Runtime but cannot wrap a proxy.
type SPARuntime struct{ state *spaRuntimeState }

type spaRuntimeState struct {
	config SPARuntimeConfig
	encode func(context.Context, *stdhttp.Request) ([]byte, error)
	active *admission.Semaphore
	err    error
}

// DefineSPARuntime binds a concrete DTO codec and concurrency-safe loader. The
// loader borrows the request, must honor cancellation, and must not retain or mutate
// it. Dependencies belong in the loader's closure, not in a context service bag.
// Registration validates the declaration before serving. Panic and Goexit are
// contained; ownership and capacity remain held until the callback actually exits.
func DefineSPARuntime[T any](codec contract.JSON[T], load func(context.Context, *stdhttp.Request) (T, error), config SPARuntimeConfig) SPARuntime {
	s := &spaRuntimeState{config: config}
	s.err = config.Validate()
	if s.err == nil {
		s.err = codec.Validate()
	}
	if s.err == nil && load == nil {
		s.err = safeerror.NewFault(fault.Invalid, "SPA runtime requires a loader")
	}
	if s.err == nil {
		s.active = admission.New(config.MaxConcurrent)
		s.encode = func(ctx context.Context, r *stdhttp.Request) ([]byte, error) {
			payload, err := load(ctx, r)
			if err != nil {
				return nil, err
			}
			return codec.Encode(ctx, payload, config.JSON)
		}
	}
	return SPARuntime{state: s}
}

// Validate checks the descriptor without invoking the loader.
func (d SPARuntime) Validate() error {
	if d.state == nil {
		return safeerror.NewFault(fault.Invalid, "SPA runtime requires a descriptor")
	}
	return d.state.err
}

func spaHTML(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	return err == nil && media == "text/html"
}

func (s *spaRuntimeState) read(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(s.config.HTMLBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > s.config.HTMLBytes {
		return nil, safeerror.NewFault(fault.Invalid, "SPA runtime HTML exceeds limit")
	}
	return data, nil
}

func (s *spaRuntimeState) document(r *stdhttp.Request, source []byte) ([]byte, error) {
	// Tokenization locates a real head tag, including attributes/case variations,
	// without matching comments or script text or reserializing the document.
	z := htmltoken.NewTokenizer(bytes.NewReader(source))
	z.SetMaxBuf(s.config.HTMLBytes)
	offset, insertion := 0, -1
	for {
		kind := z.Next()
		if kind == htmltoken.ErrorToken {
			if z.Err() != io.EOF {
				return nil, z.Err()
			}
			break
		}
		offset += len(z.Raw())
		if kind != htmltoken.StartTagToken && kind != htmltoken.SelfClosingTagToken {
			continue
		}
		token := z.Token()
		if insertion < 0 && token.Data != "head" && token.Data != "html" {
			return nil, safeerror.NewFault(fault.Invalid, "SPA runtime head must precede document content")
		}
		if token.Data == "head" && insertion < 0 && kind == htmltoken.StartTagToken {
			insertion = offset
		}
		for _, attr := range token.Attr {
			if attr.Key == "id" && attr.Val == s.config.ElementID {
				return nil, safeerror.NewFault(fault.Invalid, "SPA runtime element already exists")
			}
		}
	}
	if insertion < 0 {
		return nil, safeerror.NewFault(fault.Invalid, "SPA runtime document requires an explicit head element")
	}
	var data []byte
	err := callback.Isolated("HTTP SPA runtime loader", func() error {
		if err := r.Context().Err(); err != nil {
			return err
		}
		var err error
		data, err = s.encode(r.Context(), r)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	var escaped bytes.Buffer
	json.HTMLEscape(&escaped, data)
	if escaped.Len() > s.config.JSON.Bytes {
		return nil, safeerror.NewFault(fault.Invalid, "SPA runtime escaped JSON exceeds limit")
	}
	nonce, carrier := "", ""
	if value, ok := CSPNonceFromContext(r.Context()); ok {
		text := value.String()
		if len(text) > 256 || strings.ContainsAny(text, "\r\n") {
			return nil, safeerror.NewFault(fault.Invalid, "invalid SPA runtime nonce")
		}
		if _, err := base64.StdEncoding.Strict().DecodeString(text); err != nil {
			return nil, safeerror.NewFault(fault.Invalid, "invalid SPA runtime nonce")
		}
		nonce = ` nonce="` + html.EscapeString(text) + `"`
		if s.config.StyleNonceCarrier {
			carrier = `<style` + nonce + `></style>`
		}
	}
	script := carrier + `<script id="` + html.EscapeString(s.config.ElementID) + `"` + nonce + ` type="application/json">` + escaped.String() + `</script>`
	result := make([]byte, 0, len(source)+len(script))
	result = append(result, source[:insertion]...)
	result = append(result, script...)
	return append(result, source[insertion:]...), nil
}

func spaRuntimeHeaders(header stdhttp.Header, length int) {
	for _, name := range []string{"ETag", "Last-Modified", "Accept-Ranges", "Content-Range", "Content-Length", "Content-Encoding", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest"} {
		header.Del(name)
	}
	header.Set("Cache-Control", "no-store")
	if length >= 0 {
		header.Set("Content-Length", strconv.Itoa(length))
	}
}

func spaRuntimeError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	spaRuntimeHeaders(w.Header(), -1)
	w.Header().Del("Content-Type")
	writeRoutingError(w, r, Unavailable.WithCause(safeerror.Prefix("SPA runtime document failed", err)))
}

func (s *spaFallback) serveRuntime(w stdhttp.ResponseWriter, r *stdhttp.Request, selection assetSelection) {
	runtime := s.config.Runtime.state
	if err := runtime.active.Acquire(r.Context(), 0, nil); err != nil {
		spaRuntimeError(w, r, err)
		return
	}
	defer runtime.active.Release()
	var body []byte
	err := callback.Isolated("HTTP SPA runtime document", func() (err error) {
		content, err := s.assets.openFile(r.Context(), selection.name, false)
		if err != nil {
			return err
		}
		defer func() { err = safeerror.Join("SPA runtime file close", err, content.Body.Close()) }()
		body, err = runtime.read(content.Body)
		if err != nil {
			return err
		}
		body, err = runtime.document(r, body)
		return err
	})
	if err != nil {
		spaRuntimeError(w, r, err)
		return
	}
	clearResponseRepresentation(w.Header())
	spaRuntimeHeaders(w.Header(), len(body))
	w.Header().Set("Content-Type", string(s.assets.config.media(selection.name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if selection.varyAccept {
		appendVary(w.Header(), "Accept")
	}
	w.WriteHeader(stdhttp.StatusOK)
	if r.Method != stdhttp.MethodHead {
		if _, err := w.Write(body); err != nil {
			logRouteFailure(r, "HTTP SPA runtime write failed", safeerror.Prefix("SPA runtime write", err))
			panic(stdhttp.ErrAbortHandler)
		}
	}
}
