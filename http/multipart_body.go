package http

import (
	"io"
	"mime"
	"mime/multipart"
	stdhttp "net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/upload"
)

type multipartInput struct {
	text   url.Values
	values map[string][]multipartValue
	counts []int
}

// multipartSource observes the framework-owned body ceiling directly, without
// walking or formatting arbitrary error chains supplied by an incoming reader.
type multipartSource struct {
	source   io.Reader
	tooLarge bool
}

func (r *multipartSource) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if _, ok := err.(*stdhttp.MaxBytesError); ok {
		r.tooLarge = true
	}
	return n, err
}
func (r *multipartSource) failure(err error) error {
	if r.tooLarge || err == multipart.ErrMessageTooLarge {
		return PayloadTooLarge.WithCause(err)
	}
	return BadRequest.WithCause(err)
}

func (d Multipart[B]) read(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return *new(B), nil, RequestTimeout.WithCause(err)
	}
	if r.ContentLength > limits.Multipart.Bytes {
		rejectUnreadBody(w, r)
		return *new(B), nil, PayloadTooLarge
	}
	boundary, ok := multipartRequestMedia(r.Header)
	if !ok {
		rejectUnreadBody(w, r)
		return *new(B), nil, UnsupportedMediaType
	}
	if r.Body == nil || r.Body == stdhttp.NoBody {
		return *new(B), nil, BadRequest
	}
	config := upload.Config{TempDirectory: d.directory, MaxBytes: limits.Multipart.Bytes, MaxFileBytes: limits.Multipart.FileBytes, MaxFiles: limits.Multipart.Files, MaxReaders: limits.Multipart.Readers}
	batch, err := upload.New(ctx, config)
	if err != nil {
		return *new(B), nil, InternalError.WithCause(err)
	}
	close := batch.Close
	source := &multipartSource{source: stdhttp.MaxBytesReader(w, r.Body, limits.Multipart.Bytes)}
	input := multipartInput{text: make(url.Values), values: make(map[string][]multipartValue), counts: make([]int, len(d.parts))}
	var returned error
	owned := callback.Isolated("read multipart form", func() error {
		returned = d.readParts(r, multipart.NewReader(source, boundary), source, batch, limits, &input)
		return nil
	})
	if owned != nil {
		rejectUnreadBody(w, r)
		logRouteFailure(r, "HTTP multipart reader failed", owned)
		return *new(B), close, InternalError.WithCause(owned)
	}
	if err := ctx.Err(); err != nil {
		return *new(B), close, RequestTimeout.WithCause(err)
	}
	if returned != nil {
		rejectUnreadBody(w, r)
		return *new(B), close, returned
	}
	result, err := d.bind(ctx, input, limits)
	if err != nil {
		return *new(B), close, endpointInputError(ctx, "body", err)
	}
	return result, close, nil
}

func (d Multipart[B]) readParts(r *stdhttp.Request, reader *multipart.Reader, source *multipartSource, batch *upload.Batch, limits EndpointLimits, input *multipartInput) error {
	remainingFields := limits.Multipart.FieldsBytes
	count := 0
	for {
		if err := r.Context().Err(); err != nil {
			return RequestTimeout.WithCause(err)
		}
		part, err := reader.NextRawPart()
		if err == io.EOF {
			// MIME epilogues are legal, but their bytes still count against the
			// endpoint's complete-body ceiling. The MIME reader may have already
			// buffered some epilogue bytes; source has counted those already.
			if _, err := io.Copy(io.Discard, source); err != nil {
				return source.failure(err)
			}
			return nil
		}
		if err != nil {
			return source.failure(err)
		}
		count++
		if count > limits.Multipart.Parts {
			return PayloadTooLarge
		}
		header := stdhttp.Header(part.Header)
		if !multipartHeaderFits(header, limits.Multipart.HeaderBytes) {
			return PayloadTooLarge
		}
		dispositions := header.Values("Content-Disposition")
		if len(dispositions) != 1 {
			return BadRequest
		}
		disposition, parameters, err := mime.ParseMediaType(dispositions[0])
		if err != nil || disposition != "form-data" {
			return BadRequest
		}
		name := parameters["name"]
		index, known := d.indexes[name]
		if !known {
			return endpointInputError(r.Context(), "body", multipartIssue("", contract.UnknownIssue))
		}
		declaration := d.parts[index]
		// Retain the declared name, not a substring of an incoming MIME header.
		name = declaration.info.Name
		input.counts[index]++
		if code := parameterCardinalityIssue(declaration.info, input.counts[index]); code != "" {
			return endpointInputError(r.Context(), "body", multipartIssue(queryIssuePath(name), code))
		}
		filename, isFile := parameters["filename"]
		if isFile != (declaration.kind == MultipartFile) || len(header.Values("Content-Transfer-Encoding")) != 0 || !identityRequestEncoding(header) {
			return endpointInputError(r.Context(), "body", multipartIssue(queryIssuePath(name), contract.TypeIssue))
		}
		types := header.Values("Content-Type")
		if len(types) > 1 {
			return endpointInputError(r.Context(), "body", multipartIssue(queryIssuePath(name), contract.TypeIssue))
		}
		if declaration.kind == MultipartFile {
			file, err := batch.Capture(r.Context(), part, filename, header.Get("Content-Type"))
			if err != nil {
				switch err.(type) {
				case *upload.LimitError:
					return PayloadTooLarge.WithCause(err)
				case *upload.ReadError:
					return source.failure(err)
				default:
					return InternalError.WithCause(err)
				}
			}
			input.values[name] = append(input.values[name], multipartValue{file: file})
			continue
		}
		if declaration.kind == MultipartJSON && !jsonRequestMedia(header) || declaration.kind == MultipartText && !multipartTextMedia(header) {
			return endpointInputError(r.Context(), "body", multipartIssue(queryIssuePath(name), contract.TypeIssue))
		}
		ceiling := min(limits.Multipart.FieldBytes, remainingFields)
		if declaration.kind == MultipartJSON {
			ceiling = min(ceiling, limits.Body.Bytes)
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(ceiling)+1))
		if err != nil {
			return source.failure(err)
		}
		if len(data) > ceiling {
			return PayloadTooLarge
		}
		remainingFields -= len(data)
		if declaration.kind == MultipartText {
			if !utf8.Valid(data) {
				return endpointInputError(r.Context(), "body", multipartIssue(queryIssuePath(name), contract.ValueIssue))
			}
			input.text[name] = append(input.text[name], string(data))
		} else {
			input.values[name] = append(input.values[name], multipartValue{data: data})
		}
	}
}

func multipartRequestMedia(header stdhttp.Header) (string, bool) {
	types := header.Values("Content-Type")
	if len(types) != 1 || !identityRequestEncoding(header) {
		return "", false
	}
	media, parameters, err := mime.ParseMediaType(types[0])
	boundary := parameters["boundary"]
	if err != nil || media != "multipart/form-data" || len(boundary) == 0 || len(boundary) > 70 {
		return "", false
	}
	return boundary, true
}

func multipartTextMedia(header stdhttp.Header) bool {
	types := header.Values("Content-Type")
	if len(types) == 0 {
		return true
	}
	if len(types) != 1 {
		return false
	}
	media, parameters, err := mime.ParseMediaType(types[0])
	if err != nil || media != "text/plain" {
		return false
	}
	charset, ok := parameters["charset"]
	return !ok || strings.EqualFold(charset, "utf-8")
}

func (b Body[B]) readOwned(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
	if b.kind == payloadForm {
		result, err := b.readForm(w, r, limits.Form)
		return result, nil, err
	}

	if b.kind == payloadMultipart {
		return b.multipart.read(w, r, limits)
	}
	body, err := b.read(w, r, limits.Body)
	return body, nil, err
}

// Count canonical header lines including colon/space and CRLF. Native multipart
// parsing owns its pre-allocation bound; this check also bounds metadata retained
// by uploaded-file handles when the complete-body ceiling permits large files.
func multipartHeaderFits(header stdhttp.Header, limit int) bool {
	remaining := limit - 2
	if remaining < 0 {
		return false
	}
	for name, values := range header {
		for _, value := range values {
			if len(name) > remaining-4 {
				return false
			}
			remaining -= len(name) + 4
			if len(value) > remaining {
				return false
			}
			remaining -= len(value)
		}
	}
	return true
}
