package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	maskedValue   = "[masked]"
	maxJSONDepth  = 64
	maxJSONTokens = 20_000
	maxFormFields = 128
	maxParts      = 64
	maxPartText   = 512
)

// policy is the immutable disclosure input derived once from Config.
type policy struct {
	forceRedacted bool
	valueBytes    int
	bodyBytes     int
}

func (c Config) policy() policy {
	return policy{forceRedacted: c.ForceRedacted, valueBytes: c.ValueBytes, bodyBytes: c.BodyBytes}
}

var (
	errMalformed = safeerror.NewFault(fault.Invalid, "malformed capture body")
	errLimit     = safeerror.NewFault(fault.Invalid, "capture redaction limit")
)

// redact is the single disclosure owner: it turns one kernel snapshot into
// the stored row without I/O. Input it cannot represent safely is omitted with
// a reason, never stored raw. Header and query masking apply in both modes.
func redact(s httpcapture.Snapshot, p policy) (store.Capture, error) {
	m, c := s.Metadata, s.Completion
	id, err := model.ParseID[store.Capture](string(m.RequestID))
	if err != nil {
		return store.Capture{}, err
	}
	received, err := temporal.NewDateTime(m.ReceivedAt)
	if err != nil {
		return store.Capture{}, err
	}
	var omissions []CaptureOmission
	omit := func(present bool, omission CaptureOmission) {
		if present {
			omissions = append(omissions, omission)
		}
	}
	query, queryTruncated, queryValid := redactFields(m.RawQuery, m.QueryTruncated, p, sensitiveQueryName)
	if !queryValid {
		query = nil
	}
	omit(m.MethodTruncated, MethodTruncated)
	omit(m.PathTruncated, PathTruncated)
	omit(queryTruncated, QueryTruncated)
	omit(!queryValid, QueryMalformed)
	omit(m.HeadersTruncated, HeadersTruncated)
	omit(m.ContentTypeTruncated, ContentTypeTruncated)
	omit(m.ContentEncodingTruncated, ContentEncodingTruncated)
	omit(c.ResponseTruncated, ResponseTruncated)
	omit(c.ResponseMetadataTruncated, ResponseMetadataTruncated)
	mode := Redacted
	if m.Full && !p.forceRedacted {
		mode = Full
	}
	state, omission, body := redactBody(m, s.Body, mode, p)
	row := store.Capture{
		ID: id, ReceivedAt: received, Method: storedText(m.Method, 32), Path: storedText(m.EscapedPath, 4<<10),
		ContentType: storedText(m.ContentType, 256), ContentEncoding: storedText(m.ContentEncoding, 256),
		DeclaredBytes: m.DeclaredBytes, MeasuredBytes: s.Body.MeasuredBytes, ObservedBytes: s.Body.ObservedBytes,
		BodyMode: string(mode), BodyState: string(state), Body: storedBytes(body),
		Outcome: outcome(c.Result), ResponseState: string(responseState(c)),
		ResponseContentType: storedText(c.ResponseContentType, 256), ResponseEncoding: storedText(c.ResponseEncoding, 256),
		ResponseBytes: c.ResponseBytes, ResponseHead: storedBytes(c.ResponseHead),
		DurationMS: max(0, c.Duration.Milliseconds()), DroppedBefore: int64(min(s.DroppedBefore, math.MaxInt64)),
	}
	if omission != "" {
		row.BodyOmission = value.Of(string(omission))
	}
	if c.Route != "" {
		row.Route = value.Of(storedText(c.Route, 256))
	}
	if m.ClientIP.IsValid() {
		row.ClientIP = value.Of(m.ClientIP.WithZone("").String())
	}
	if status, present := c.Status.Get(); present {
		row.ResponseStatus = value.Of(int32(status))
	}
	if row.Headers, err = value.NewJSON(redactHeaders(m.Headers)); err != nil {
		return store.Capture{}, err
	}
	if row.Query, err = value.NewJSON(nonNil(query)); err != nil {
		return store.Capture{}, err
	}
	names := make([]string, len(omissions))
	for i, omission := range omissions {
		names[i] = string(omission)
	}
	row.Omissions, err = value.NewJSON(names)
	return row, err
}

// jsonText makes transport text storable: PostgreSQL text and jsonb reject
// invalid UTF-8 and NUL, and one bad value would fail its whole batch.
func jsonText(text string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(text, "\uFFFD"), "\x00", "\uFFFD")
}

// storedText also cuts replacement growth back to a column's byte limit on
// a rune boundary.
func storedText(text string, limit int) string {
	text = jsonText(text)
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// storedBytes keeps an absent body or response head as zero bytes: the
// columns are NOT NULL and the byte codec rejects a nil slice.
func storedBytes(data []byte) []byte {
	if data == nil {
		return []byte{}
	}
	return data
}

func outcome(result observability.Result) observability.Outcome {
	normalized, err := result.Normalized(observability.HTTP)
	if err != nil {
		return observability.Failed
	}
	return normalized.Outcome
}

func responseState(c httpcapture.Completion) CaptureResponseState {
	switch {
	case c.Hijacked:
		return ResponseHijacked
	case c.Aborted:
		return ResponseAborted
	case c.WriteFailed:
		return ResponseWriteFailed
	default:
		return ResponseComplete
	}
}

func redactBody(m httpcapture.Metadata, b httpcapture.Body, mode CaptureMode, p policy) (CaptureBodyState, CaptureBodyOmission, []byte) {
	switch b.End {
	case httpcapture.NotRead:
		return BodyOmitted, OmittedUnread, nil
	case httpcapture.OverCaptureLimit:
		return BodyOmitted, OmittedOverCaptureLimit, nil
	case httpcapture.SourceFailure:
		switch b.ReadFailure {
		case httpcapture.BodyLimit:
			return BodyOmitted, OmittedBodyLimit, nil
		case httpcapture.ClientDisconnect:
			return BodyOmitted, OmittedClientDisconnect, nil
		case httpcapture.ReadTimeout:
			return BodyOmitted, OmittedReadTimeout, nil
		default:
			return BodyOmitted, OmittedReadFailed, nil
		}
	}
	if len(b.Prefix) == 0 {
		return BodyEmpty, "", nil
	}
	if mode == Full {
		return BodyExact, "", b.Prefix
	}
	if encoding := strings.TrimSpace(m.ContentEncoding); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return BodyOmitted, OmittedContentEncoded, nil
	}
	media, params, err := mime.ParseMediaType(m.ContentType)
	if err != nil {
		return BodyOmitted, OmittedUnsupportedMediaType, nil
	}
	var state CaptureBodyState
	var out []byte
	switch {
	case media == "application/json" || strings.HasSuffix(media, "+json"):
		state = BodyJSON
		out, err = redactJSON(b.Prefix, p)
	case media == "application/x-www-form-urlencoded":
		state = BodyForm
		fields, over, valid := redactFields(string(b.Prefix), false, p, record.SensitiveName)
		switch {
		case !valid:
			err = errMalformed
		case over:
			err = errLimit
		default:
			out, err = json.Marshal(nonNil(fields))
		}
	case media == "multipart/form-data":
		state = BodyMultipart
		out, err = summarizeMultipart(b.Prefix, params["boundary"], p)
	default:
		return BodyOmitted, OmittedUnsupportedMediaType, nil
	}
	switch {
	case errors.Is(err, errLimit) || (err == nil && len(out) > p.bodyBytes):
		return BodyOmitted, OmittedRedactionLimit, nil
	case err != nil:
		return BodyOmitted, OmittedMalformed, nil
	}
	return state, "", out
}

func sensitiveQueryName(name string) bool {
	return record.SensitiveName(name) || http.CredentialQueryName(name)
}

func redactHeaders(fields []httpcapture.Field) []store.Field {
	result := make([]store.Field, len(fields))
	for i, field := range fields {
		values := make([]string, len(field.Values))
		for j, v := range field.Values {
			values[j] = jsonText(v)
			if record.SensitiveName(field.Name) {
				values[j] = maskedValue
			}
		}
		result[i] = store.Field{Name: jsonText(field.Name), Values: values}
	}
	return result
}

// redactFields parses URL-encoded pairs in first-appearance order, grouping
// repeated names. A truncated input drops its possibly partial last pair. It
// reports false for any malformed escape instead of keeping raw text.
func redactFields(raw string, truncated bool, p policy, sensitive func(string) bool) ([]store.Field, bool, bool) {
	if truncated {
		if cut := strings.LastIndexByte(raw, '&'); cut >= 0 {
			raw = raw[:cut]
		} else {
			raw = ""
		}
	}
	var fields []store.Field
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
			fields = append(fields, store.Field{Name: jsonText(name)})
		}
		fields[at].Values = append(fields[at].Values, jsonText(redactValue(name, text, p, sensitive)))
	}
	return fields, truncated, true
}

func redactValue(name, text string, p policy, sensitive func(string) bool) string {
	switch {
	case sensitive(name):
		return maskedValue
	case len(text) > p.valueBytes:
		return omittedText(len(text))
	default:
		return text
	}
}

func omittedText(size int) string { return "[omitted " + strconv.Itoa(size) + " bytes]" }

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// redactJSON re-encodes a single JSON document token by token, so duplicate
// keys and number spellings survive. Values under credential-named keys are
// masked whole; long strings are replaced by their decoded size.
func redactJSON(raw []byte, p policy) ([]byte, error) {
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
			if out.Len() > p.bodyBytes {
				return nil, errLimit
			}
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, errMalformed
		}
		if tokens++; tokens > maxJSONTokens || out.Len() > p.bodyBytes {
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
			if err := writeString(maskedValue); err != nil {
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
				maskNext = record.SensitiveName(v)
				err = writeString(v)
			} else if len(v) > p.valueBytes {
				err = writeString(omittedText(len(v)))
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
func summarizeMultipart(raw []byte, boundary string, p policy) ([]byte, error) {
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
			summary.Value = value.Set(redactValue(part.FormName(), content.String(), p, record.SensitiveName))
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
