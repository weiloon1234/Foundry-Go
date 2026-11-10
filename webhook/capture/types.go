package capture

import (
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CaptureMode is the body disclosure policy a capture was stored under.
//
//foundry:enum
type CaptureMode string

const (
	// Redacted masks credential-named fields, shortens long values and
	// summarizes multipart files. It is the default for every prefix.
	Redacted CaptureMode = "redacted"
	// Full stores complete bodies within the capture limit exactly as received.
	// Only a Go declaration with http.FullCaptureBody selects it.
	Full CaptureMode = "full"
)

// CaptureBodyState says what a capture's stored body holds.
//
//foundry:enum
type CaptureBodyState string

const (
	// BodyEmpty is a complete request without body bytes.
	BodyEmpty CaptureBodyState = "empty"
	// BodyExact holds the complete body bytes exactly as received (full mode).
	BodyExact CaptureBodyState = "exact"
	// BodyJSON holds the redacted JSON document.
	BodyJSON CaptureBodyState = "json"
	// BodyForm holds redacted URL-encoded fields as a JSON array of
	// {"name","values"} objects.
	BodyForm CaptureBodyState = "form"
	// BodyMultipart holds a JSON array describing each part; file contents
	// are never stored.
	BodyMultipart CaptureBodyState = "multipart"
	// BodyOmitted stores nothing; CaptureBodyOmission says why.
	BodyOmitted CaptureBodyState = "omitted"
)

// CaptureBodyOmission explains why a body was not stored. Completeness comes
// only from an observed end of stream, never from a successful parse.
//
//foundry:enum
type CaptureBodyOmission string

const (
	// OmittedUnread: the request finished before capture read its body, as
	// with maintenance, capacity or declared-length rejections.
	OmittedUnread CaptureBodyOmission = "unread"
	// OmittedOverCaptureLimit: the body exceeded the configured capture limit.
	OmittedOverCaptureLimit CaptureBodyOmission = "over_capture_limit"
	// OmittedBodyLimit: the server body limit stopped reading.
	OmittedBodyLimit CaptureBodyOmission = "body_limit"
	// OmittedClientDisconnect: the client closed the stream early.
	OmittedClientDisconnect CaptureBodyOmission = "client_disconnect"
	// OmittedReadTimeout: the read deadline expired.
	OmittedReadTimeout CaptureBodyOmission = "read_timeout"
	// OmittedReadFailed: another transport read failure.
	OmittedReadFailed CaptureBodyOmission = "read_failed"
	// OmittedContentEncoded: redacted mode cannot inspect compressed bodies.
	OmittedContentEncoded CaptureBodyOmission = "content_encoded"
	// OmittedUnsupportedMediaType: redacted mode stores only JSON, URL-encoded
	// and multipart form bodies.
	OmittedUnsupportedMediaType CaptureBodyOmission = "unsupported_media_type"
	// OmittedMalformed: the body did not parse as its declared media type and
	// is never stored raw.
	OmittedMalformed CaptureBodyOmission = "malformed"
	// OmittedRedactionLimit: nesting, token, part or output bounds were exceeded.
	OmittedRedactionLimit CaptureBodyOmission = "redaction_limit"
)

// CaptureResponseState records how the handler's response ended. It
// describes writes the server accepted, not delivery to the caller.
//
//foundry:enum
type CaptureResponseState string

const (
	// ResponseComplete: the handler returned and every write was accepted.
	ResponseComplete CaptureResponseState = "complete"
	// ResponseWriteFailed: a response write failed.
	ResponseWriteFailed CaptureResponseState = "write_failed"
	// ResponseAborted: a raw panic aborted the connection.
	ResponseAborted CaptureResponseState = "aborted"
	// ResponseHijacked: the handler took over the connection.
	ResponseHijacked CaptureResponseState = "hijacked"
)

// CaptureOmission marks request or response metadata that was cut to its
// bound or left out.
//
//foundry:enum
type CaptureOmission string

const (
	MethodTruncated           CaptureOmission = "method_truncated"
	PathTruncated             CaptureOmission = "path_truncated"
	QueryTruncated            CaptureOmission = "query_truncated"
	QueryMalformed            CaptureOmission = "query_malformed"
	HeadersTruncated          CaptureOmission = "headers_truncated"
	ContentTypeTruncated      CaptureOmission = "content_type_truncated"
	ContentEncodingTruncated  CaptureOmission = "content_encoding_truncated"
	ResponseTruncated         CaptureOmission = "response_truncated"
	ResponseMetadataTruncated CaptureOmission = "response_metadata_truncated"
)

// CapturedRequestFilters narrows the list. From is required and the window
// spans at most 30 days; Until defaults to no upper bound.
//
// Example: capture.CapturedRequestFilters{From: since, Method: value.Set("POST")}
//
//foundry:query
type CapturedRequestFilters struct {
	From  temporal.DateTime                 `query:"from"`
	Until value.Optional[temporal.DateTime] `query:"until"`
	// Method matches the request method exactly.
	Method value.Optional[string] `query:"method"`
	// PathPrefix matches captures whose escaped path starts with this text.
	PathPrefix value.Optional[string]      `query:"path_prefix"`
	Status     value.Optional[int32]       `query:"status"`
	Mode       value.Optional[CaptureMode] `query:"mode"`
}

// CapturedRequestPath binds the show route's request ID.
//
//foundry:path pattern=/{request}
type CapturedRequestPath struct {
	Request attribution.RequestID `path:"request"`
}

// CapturedRequest is one list item and the head of a detail. It never
// carries headers, query values or bodies.
//
//foundry:dto
type CapturedRequest struct {
	RequestID  attribution.RequestID `json:"request_id"`
	ReceivedAt temporal.DateTime     `json:"received_at"`
	Method     string                `json:"method"`
	// Path is the escaped request path as sent, at most 4 KiB.
	Path string `json:"path"`
	// Route is the matched route ID; null when no route matched.
	Route    value.Nullable[string] `json:"route"`
	ClientIP value.Nullable[string] `json:"client_ip"`
	// Status is the committed response status; null when none was sent.
	Status        value.Nullable[int32] `json:"status"`
	Outcome       observability.Outcome `json:"outcome"`
	ResponseState CaptureResponseState  `json:"response_state"`
	DurationMS    int64                 `json:"duration_ms"`
	Mode          CaptureMode           `json:"mode"`
	BodyState     CaptureBodyState      `json:"body_state"`
	// BodyOmission is set exactly when BodyState is omitted.
	BodyOmission value.Nullable[CaptureBodyOmission] `json:"body_omission"`
	// DeclaredBytes is the request Content-Length; null when absent.
	DeclaredBytes value.Nullable[int64] `json:"declared_bytes"`
	// MeasuredBytes is the body size, known only after the end of the stream.
	MeasuredBytes value.Nullable[int64] `json:"measured_bytes"`
	// ObservedBytes counts body bytes read from the client.
	ObservedBytes int64 `json:"observed_bytes"`
	// DroppedBefore counts captures the writing process lost after its
	// previous stored capture and before this one.
	DroppedBefore int64 `json:"dropped_before"`
}

// CapturedField is one header, query or form name with its values. Masked
// credential values read "[masked]".
//
//foundry:dto
type CapturedField struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// CapturedRequestDetail is one capture with its stored metadata and bodies.
// Body and ResponseHead are base64 in JSON. The response head is the first
// 4 KiB the server accepted for writing; it is not redacted.
//
//foundry:dto
type CapturedRequestDetail struct {
	Request             CapturedRequest `json:"request"`
	Headers             []CapturedField `json:"headers"`
	Query               []CapturedField `json:"query"`
	ContentType         string          `json:"content_type"`
	ContentEncoding     string          `json:"content_encoding"`
	Body                []byte          `json:"body"`
	ResponseContentType string          `json:"response_content_type"`
	ResponseEncoding    string          `json:"response_encoding"`
	ResponseBytes       int64           `json:"response_bytes"`
	ResponseHead        []byte          `json:"response_head"`
	// ResponseHeaders are the headers the application set when the response
	// was committed, unmasked. Transport headers net/http adds afterwards
	// (Date, Content-Length, a sniffed Content-Type) are not included.
	ResponseHeaders []CapturedField   `json:"response_headers"`
	Omissions       []CaptureOmission `json:"omissions"`
}
