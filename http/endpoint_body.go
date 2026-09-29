package http

import (
	"io"
	"mime"
	stdhttp "net/http"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

func (b Body[B]) read(w stdhttp.ResponseWriter, r *stdhttp.Request, limits contract.JSONLimits) (B, error) {
	data, err := readEndpointBody(w, r, int64(limits.Bytes), b.kind == payloadEmpty, jsonRequestMedia)
	if err != nil {
		return *new(B), err
	}
	if b.kind == payloadEmpty {
		return *new(B), nil
	}
	result, err := b.json.Decode(r.Context(), data, limits)
	if err != nil {
		return *new(B), endpointInputError(r.Context(), "body", err)
	}
	return result, nil
}

func readEndpointBody(w stdhttp.ResponseWriter, r *stdhttp.Request, maxBytes int64, empty bool, media func(stdhttp.Header) bool) ([]byte, error) {
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return nil, RequestTimeout.WithCause(err)
	}
	if empty {
		if r.ContentLength > 0 {
			rejectUnreadBody(w, r)
			return nil, BadRequest
		}
		if r.Body == nil || r.Body == stdhttp.NoBody {
			return nil, nil
		}
	} else {
		if r.ContentLength > maxBytes {
			rejectUnreadBody(w, r)
			return nil, PayloadTooLarge
		}
		if !media(r.Header) {
			rejectUnreadBody(w, r)
			return nil, UnsupportedMediaType
		}
	}
	var data []byte
	var readErr error
	// Framework hot path: Invoke contains reader panics on this goroutine.
	failure := callback.Invoke("HTTP endpoint body read", func() error {
		if r.Body == nil {
			return nil
		}
		if empty {
			data, readErr = io.ReadAll(io.LimitReader(r.Body, 1))
		} else {
			data, readErr = readBodySized(stdhttp.MaxBytesReader(w, r.Body, maxBytes), r.ContentLength)
		}
		return nil // Never format a reader's arbitrary error methods.
	})
	if failure != nil {
		logRouteFailure(r, "HTTP endpoint body reader failed", failure)
		rejectUnreadBody(w, r)
		return nil, InternalError.WithCause(failure)
	}
	if err := ctx.Err(); err != nil {
		return nil, RequestTimeout.WithCause(err)
	}
	if readErr != nil {
		rejectUnreadBody(w, r)
		if _, large := readErr.(*stdhttp.MaxBytesError); large {
			return nil, PayloadTooLarge.WithCause(readErr)
		}
		return nil, BadRequest.WithCause(readErr)
	}
	if empty && len(data) != 0 {
		rejectUnreadBody(w, r)
		return nil, BadRequest
	}
	return data, nil
}

// bodyPresize bounds the buffer allocated from a declared Content-Length before
// its bytes arrive, so a peer cannot reserve a whole body budget by header alone.
const bodyPresize = 32 << 10

// readBodySized reads like io.ReadAll but sizes its first buffer from the
// declared length (bounded), avoiding repeated growth for ordinary bodies.
func readBodySized(reader io.Reader, declared int64) ([]byte, error) {
	size := 512
	if declared >= 0 {
		size = int(min(declared, bodyPresize)) + 1
	}
	data := make([]byte, 0, size)
	for {
		n, err := reader.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return data, err
		}
		if len(data) == cap(data) {
			data = append(data, 0)[:len(data)]
		}
	}
}

func jsonRequestMedia(header stdhttp.Header) bool {
	return requestMedia(header, func(media string) bool {
		return media == "application/json" || strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")
	})
}

func requestMedia(header stdhttp.Header, accepts func(string) bool) bool {
	contentTypes := header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return false
	}
	media, parameters, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !accepts(media) {
		return false
	}
	if charset, ok := parameters["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return false
	}
	return identityRequestEncoding(header)
}

func identityRequestEncoding(header stdhttp.Header) bool {
	encodings := header.Values("Content-Encoding")
	return len(encodings) == 0 || len(encodings) == 1 && (strings.TrimSpace(encodings[0]) == "" || strings.EqualFold(strings.TrimSpace(encodings[0]), "identity"))
}

func rejectUnreadBody(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	// net/http owns Body.Close. Closing this HTTP/1.x connection avoids a
	// premature 100-continue or draining an intentionally unread request body.
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
}
