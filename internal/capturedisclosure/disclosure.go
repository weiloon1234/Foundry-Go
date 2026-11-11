// Package capturedisclosure is the single disclosure owner for both HTTP
// capture directions: pure functions from raw captured facts to stored values,
// with no I/O. Input it cannot represent safely is omitted with a reason,
// never stored raw.
package capturedisclosure

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Masked replaces a credential value in every mode.
const Masked = "[masked]"

const (
	maxJSONDepth  = 64
	maxJSONTokens = 20_000
	maxFormFields = 128
	maxParts      = 64
	maxPartText   = 512
)

var (
	errMalformed = safeerror.NewFault(fault.Invalid, "malformed capture body")
	errLimit     = safeerror.NewFault(fault.Invalid, "capture redaction limit")
)

// Format is what redacted mode can parse.
type Format uint8

const (
	Opaque Format = iota
	JSON
	Form
	Multipart
)

// Reason is the internal omission vocabulary; each public capture package
// maps it onto its own generated enum.
type Reason uint8

const (
	NoReason Reason = iota
	ContentEncoded
	UnsupportedMediaType
	Malformed
	RedactionLimit
)

// Media is a body's parsed redaction format.
type Media struct {
	Format   Format
	boundary string
}

// Inspectable decides, from Content-Type and Content-Encoding alone, whether
// redacted mode can store a body. Outbound capture calls it before buffering
// and redaction calls it again, so the two never disagree.
func Inspectable(contentType, contentEncoding string) (Media, Reason) {
	if encoding := strings.TrimSpace(contentEncoding); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return Media{}, ContentEncoded
	}
	media, params, err := mime.ParseMediaType(contentType)
	switch {
	case err != nil:
		return Media{}, UnsupportedMediaType
	case media == "application/json" || strings.HasSuffix(media, "+json"):
		return Media{Format: JSON}, NoReason
	case media == "application/x-www-form-urlencoded":
		return Media{Format: Form}, NoReason
	case media == "multipart/form-data":
		return Media{Format: Multipart, boundary: params["boundary"]}, NoReason
	default:
		return Media{}, UnsupportedMediaType
	}
}

// Policy is the immutable redaction input: value and output bounds and the
// extra credential names deployment settings add.
type Policy struct {
	ValueBytes int
	BodyBytes  int
	Names      Names
}

func (p Policy) credential(name string) bool { return CredentialName(name, p.Names) }

// Redact rewrites one complete body of an inspectable media type. Callers
// decide completeness from an observed end of stream first; a successful
// parse never proves completeness.
func Redact(data []byte, media Media, p Policy) ([]byte, Reason) {
	var out []byte
	var err error
	switch media.Format {
	case JSON:
		out, err = redactJSON(data, p)
	case Form:
		fields, over, valid := Fields(string(data), false, p, p.credential)
		switch {
		case !valid:
			err = errMalformed
		case over:
			err = errLimit
		default:
			out, err = json.Marshal(NonNil(fields))
		}
	case Multipart:
		out, err = summarizeMultipart(data, media.boundary, p)
	default:
		return nil, UnsupportedMediaType
	}
	switch {
	case errors.Is(err, errLimit) || (err == nil && len(out) > p.BodyBytes):
		return nil, RedactionLimit
	case err != nil:
		return nil, Malformed
	}
	return out, NoReason
}

// Text makes transport text storable: PostgreSQL text and jsonb reject
// invalid UTF-8 and NUL, and one bad value would fail its whole batch.
func Text(text string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(text, "�"), "\x00", "�")
}

// TextLimit also cuts replacement growth back to a column's byte limit on a
// rune boundary.
func TextLimit(text string, limit int) string {
	text = Text(text)
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// Bytes keeps an absent body as zero bytes: body columns are NOT NULL and the
// byte codec rejects a nil slice.
func Bytes(data []byte) []byte {
	if data == nil {
		return []byte{}
	}
	return data
}

// NonNil stores an absent list as an empty JSON array.
func NonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// Headers masks the values of names mask matches and makes text storable.
// Webhook response headers pass a mask that matches nothing: they are what
// the application sent, kept whole for debugging.
func Headers(fields []httpcapture.Field, mask func(string) bool) []httpcapture.Field {
	result := make([]httpcapture.Field, len(fields))
	for i, field := range fields {
		values := make([]string, len(field.Values))
		masked := mask(field.Name)
		for j, v := range field.Values {
			values[j] = Text(v)
			if masked {
				values[j] = Masked
			}
		}
		result[i] = httpcapture.Field{Name: Text(field.Name), Values: values}
	}
	return result
}

// Fields parses URL-encoded pairs in first-appearance order, grouping
// repeated names and masking values whose names mask matches. A truncated
// input drops its possibly partial last pair. It reports false for any
// malformed escape instead of keeping raw text, and cut when the field bound
// stopped it.
func Fields(raw string, truncated bool, p Policy, mask func(string) bool) (fields []httpcapture.Field, cut bool, valid bool) {
	if truncated {
		if at := strings.LastIndexByte(raw, '&'); at >= 0 {
			raw = raw[:at]
		} else {
			raw = ""
		}
	}
	index := make(map[string]int)
	for pair := range strings.SplitSeq(raw, "&") {
		if pair == "" {
			continue
		}
		rawName, rawValue, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(rawName)
		if err != nil {
			return nil, truncated, false
		}
		text, err := url.QueryUnescape(rawValue)
		if err != nil {
			return nil, truncated, false
		}
		at, seen := index[name]
		if !seen {
			if len(fields) == maxFormFields {
				return fields, true, true
			}
			at = len(fields)
			index[name] = at
			fields = append(fields, httpcapture.Field{Name: Text(name)})
		}
		fields[at].Values = append(fields[at].Values, Text(redactValue(name, text, p, mask)))
	}
	return fields, truncated, true
}

func redactValue(name, text string, p Policy, mask func(string) bool) string {
	switch {
	case mask(name):
		return Masked
	case len(text) > p.ValueBytes:
		return Omitted(len(text))
	default:
		return text
	}
}

// Omitted describes a value too long to store.
func Omitted(size int) string { return "[omitted " + strconv.Itoa(size) + " bytes]" }

// redactJSON re-encodes a single JSON document token by token, so duplicate
// keys and number spellings survive. Values under credential-named keys are
// masked whole; long strings are replaced by their decoded size.
func redactJSON(raw []byte, p Policy) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out bytes.Buffer
	type frame struct {
		object, key, first bool
	}
	var stack []frame
	tokens, masking, maskDepth := 0, false, 0
	writeString := func(text string) error {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(text); err != nil {
			return errMalformed
		}
		out.Write(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
		return nil
	}
	// separate writes the comma or colon owed before the next token and
	// reports whether that token is an object key.
	separate := func() bool {
		if len(stack) == 0 {
			return false
		}
		top := &stack[len(stack)-1]
		key := top.object && !top.key
		switch {
		case top.object && top.key:
			out.WriteByte(':')
		case !top.first:
			out.WriteByte(',')
		}
		top.first = false
		if top.object {
			top.key = !top.key
		}
		return key
	}
	maskNext := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if len(stack) != 0 || tokens == 0 {
				return nil, errMalformed
			}
			if out.Len() > p.BodyBytes {
				return nil, errLimit
			}
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, errMalformed
		}
		if tokens++; tokens > maxJSONTokens || out.Len() > p.BodyBytes {
			return nil, errLimit
		}
		if tokens > 1 && len(stack) == 0 && !masking {
			return nil, errMalformed
		}
		if masking {
			if delim, ok := token.(json.Delim); ok {
				if delim == '{' || delim == '[' {
					maskDepth++
				} else {
					maskDepth--
				}
			}
			if maskDepth == 0 {
				masking = false
				if len(stack) == 0 {
					continue
				}
			}
			continue
		}
		if delim, ok := token.(json.Delim); ok && (delim == '}' || delim == ']') {
			stack = stack[:len(stack)-1]
			out.WriteByte(byte(delim))
			continue
		}
		key := separate()
		if maskNext {
			maskNext = false
			if err := writeString(Masked); err != nil {
				return nil, err
			}
			if delim, ok := token.(json.Delim); ok && (delim == '{' || delim == '[') {
				masking, maskDepth = true, 1
			}
			continue
		}
		switch v := token.(type) {
		case json.Delim:
			if len(stack) == maxJSONDepth {
				return nil, errLimit
			}
			stack = append(stack, frame{object: v == '{', first: true})
			out.WriteByte(byte(v))
		case string:
			if key {
				maskNext = p.credential(v)
				err = writeString(v)
			} else if len(v) > p.ValueBytes {
				err = writeString(Omitted(len(v)))
			} else {
				err = writeString(v)
			}
			if err != nil {
				return nil, err
			}
		case json.Number:
			out.WriteString(v.String())
		case bool:
			out.WriteString(strconv.FormatBool(v))
		case nil:
			out.WriteString("null")
		default:
			return nil, errMalformed
		}
	}
}

type multipartPart struct {
	Name        string                 `json:"name"`
	Filename    value.Optional[string] `json:"filename,omitzero"`
	ContentType string                 `json:"content_type,omitempty"`
	Bytes       int64                  `json:"bytes"`
	Value       value.Optional[string] `json:"value,omitzero"`
}

// summarizeMultipart describes each part. File parts keep their submitted
// name, media type and measured size, never their bytes.
func summarizeMultipart(raw []byte, boundary string, p Policy) ([]byte, error) {
	if boundary == "" {
		return nil, errMalformed
	}
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	parts := []multipartPart{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errMalformed
		}
		if len(parts) == maxParts {
			return nil, errLimit
		}
		var content bytes.Buffer
		size, err := io.Copy(&content, part)
		if err != nil {
			return nil, errMalformed
		}
		name, _ := clip(part.FormName(), maxPartText)
		summary := multipartPart{Name: name, Bytes: size}
		summary.ContentType, _ = clip(part.Header.Get("Content-Type"), 256)
		if filename := part.FileName(); filename != "" {
			clipped, _ := clip(filename, maxPartText)
			summary.Filename = value.Set(clipped)
		} else {
			summary.Value = value.Set(redactValue(part.FormName(), content.String(), p, p.credential))
		}
		parts = append(parts, summary)
	}
	return json.Marshal(parts)
}

func clip(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	return text[:limit], true
}
