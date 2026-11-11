package capture

import (
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Exchange owns exchange identifiers: one stored wire attempt.
type Exchange struct{}

// Operation owns operation identifiers: one Do, Stream or Download call,
// which groups its attempts.
type Operation struct{}

// ExchangeID identifies one stored attempt.
type ExchangeID = model.ID[Exchange]

// OperationID groups the attempts of one call.
type OperationID = model.ID[Operation]

// OutboundResult is how one attempt ended. Exactly one applies per row.
//
//foundry:enum
type OutboundResult string

const (
	// ResultCompleted: response headers arrived and the attempt ended without
	// a failure, at any HTTP status, including a retried status.
	ResultCompleted OutboundResult = "completed"
	// ResultConsumerFailed: the Stream callback or Download destination failed.
	ResultConsumerFailed OutboundResult = "consumer_failed"
	// ResultResponseBodyFailed: reading the response body failed.
	ResultResponseBodyFailed OutboundResult = "response_body_failed"
	// ResultResponseTooLarge: Content-Length or the body exceeded the client's limit.
	ResultResponseTooLarge OutboundResult = "response_too_large"
	// ResultRequestBodyFailed: opening or reading the request body failed.
	ResultRequestBodyFailed OutboundResult = "request_body_failed"
	// ResultTransportFailed: connect, TLS, protocol or response header failure.
	ResultTransportFailed OutboundResult = "transport_failed"
	// ResultTimedOut: the attempt or operation deadline passed.
	ResultTimedOut OutboundResult = "timed_out"
	// ResultCanceled: the caller's context was canceled.
	ResultCanceled OutboundResult = "canceled"
	// ResultDestinationDenied: the destination policy refused the URL or address.
	ResultDestinationDenied OutboundResult = "destination_denied"
	// ResultOverloaded: the client's admission was full; nothing was sent.
	ResultOverloaded OutboundResult = "overloaded"
)

// OutboundCaptureMode is the body disclosure policy a capture was stored under.
//
//foundry:enum
type OutboundCaptureMode string

const (
	// Redacted masks credential-named fields, shortens long values and
	// summarizes multipart files. It is the default for every client.
	Redacted OutboundCaptureMode = "redacted"
	// Full stores bodies within the capture limit exactly. Only
	// application.Builder.FullOutboundCapture selects it.
	Full OutboundCaptureMode = "full"
)

// OutboundBodyState says what one stored body holds.
//
//foundry:enum
type OutboundBodyState string

const (
	// BodyEmpty is a complete body without bytes.
	BodyEmpty OutboundBodyState = "empty"
	// BodyExact holds the complete body exactly (full mode).
	BodyExact OutboundBodyState = "exact"
	// BodyExactPrefix holds the first bytes of a body that was larger than
	// the capture limit or not read to its end (full mode).
	BodyExactPrefix OutboundBodyState = "exact_prefix"
	// BodyJSON holds the redacted JSON document.
	BodyJSON OutboundBodyState = "json"
	// BodyForm holds redacted URL-encoded fields as a JSON array of
	// {"name","values"} objects.
	BodyForm OutboundBodyState = "form"
	// BodyMultipart holds a JSON array describing each part; file contents
	// are never stored.
	BodyMultipart OutboundBodyState = "multipart"
	// BodyOmitted stores nothing; OutboundBodyOmission says why.
	BodyOmitted OutboundBodyState = "omitted"
)

// OutboundBodyOmission explains why a body was not stored. Completeness comes
// only from an observed end of stream, never from a successful parse.
//
//foundry:enum
type OutboundBodyOmission string

const (
	// OmittedNotSent: the call never reached the wire (attempt 0).
	OmittedNotSent OutboundBodyOmission = "not_sent"
	// OmittedNoResponse: no response headers arrived.
	OmittedNoResponse OutboundBodyOmission = "no_response"
	// OmittedIncomplete: the body was not read to its end, as with a Stream
	// callback that stopped early or a Download of a non-2xx response.
	OmittedIncomplete OutboundBodyOmission = "incomplete"
	// OmittedOverCaptureLimit: the body exceeded config.body_bytes.
	OmittedOverCaptureLimit OutboundBodyOmission = "over_capture_limit"
	// OmittedContentEncoded: redacted mode cannot inspect compressed bodies.
	OmittedContentEncoded OutboundBodyOmission = "content_encoded"
	// OmittedUnsupportedMediaType: redacted mode stores only JSON, URL-encoded
	// and multipart form bodies.
	OmittedUnsupportedMediaType OutboundBodyOmission = "unsupported_media_type"
	// OmittedMalformed: the body did not parse as its declared media type and
	// is never stored raw.
	OmittedMalformed OutboundBodyOmission = "malformed"
	// OmittedRedactionLimit: nesting, token, part or output bounds were exceeded.
	OmittedRedactionLimit OutboundBodyOmission = "redaction_limit"
	// OmittedCaptureMemory: the capture memory budget had no room for the body.
	OmittedCaptureMemory OutboundBodyOmission = "capture_memory"
	// OmittedDeclined: the request was declared with WithoutBodyCapture.
	OmittedDeclined OutboundBodyOmission = "declined"
)

// OutboundCaptureOmission marks request or response metadata that was cut
// to its bound or left out.
//
//foundry:enum
type OutboundCaptureOmission string

const (
	PathTruncated            OutboundCaptureOmission = "path_truncated"
	QueryTruncated           OutboundCaptureOmission = "query_truncated"
	QueryMalformed           OutboundCaptureOmission = "query_malformed"
	RequestHeadersTruncated  OutboundCaptureOmission = "request_headers_truncated"
	ResponseHeadersTruncated OutboundCaptureOmission = "response_headers_truncated"
)

// OutboundCaptureFilters narrows the list. From is required and the window
// spans at most 30 days; Until defaults to no upper bound.
//
// Example: capture.OutboundCaptureFilters{From: since, Client: value.Set(httpclient.Name("smartpay"))}
//
//foundry:query
type OutboundCaptureFilters struct {
	From  temporal.DateTime                 `query:"from"`
	Until value.Optional[temporal.DateTime] `query:"until"`
	// Client matches the client name exactly.
	Client value.Optional[httpclient.Name] `query:"client"`
	// Host matches the stored host exactly, including a port when the URL had one.
	Host   value.Optional[string]         `query:"host"`
	Method value.Optional[string]         `query:"method"`
	Status value.Optional[int32]          `query:"status"`
	Result value.Optional[OutboundResult] `query:"result"`
	// Problems keeps attempts that did not complete or answered 400 or above.
	Problems value.Optional[bool] `query:"problems"`
	// RequestID keeps calls made while handling one inbound request.
	RequestID value.Optional[attribution.RequestID] `query:"request_id"`
	// Operation keeps the attempts of one call.
	Operation value.Optional[OperationID] `query:"operation"`
}

// OutboundCapturePath binds the show route's capture ID.
//
//foundry:path pattern=/{capture}
type OutboundCapturePath struct {
	Capture ExchangeID `path:"capture"`
}

// OutboundCapture is one list item and the head of a detail. It never
// carries headers, query values or bodies.
//
//foundry:dto
type OutboundCapture struct {
	ID          ExchangeID  `json:"id"`
	OperationID OperationID `json:"operation_id"`
	// Attempt is 1 to 10, or 0 for a call that never reached the wire.
	Attempt int32 `json:"attempt"`
	// Retried means the client made another attempt after this one.
	Retried   bool              `json:"retried"`
	StartedAt temporal.DateTime `json:"started_at"`
	Client    httpclient.Name   `json:"client"`
	Method    string            `json:"method"`
	Host      string            `json:"host"`
	// Path is the escaped request path, at most 4 KiB.
	Path string `json:"path"`
	// Status is the response status; null when no response arrived.
	Status value.Nullable[int32] `json:"status"`
	Result OutboundResult        `json:"result"`
	// HeadersMS is the time to response headers; null without a response.
	HeadersMS  value.Nullable[int64] `json:"headers_ms"`
	DurationMS int64                 `json:"duration_ms"`
	// RequestID is the inbound request that made the call, if any.
	RequestID         value.Nullable[attribution.RequestID] `json:"request_id"`
	Mode              OutboundCaptureMode                   `json:"mode"`
	RequestSentBytes  int64                                 `json:"request_sent_bytes"`
	ResponseReadBytes int64                                 `json:"response_read_bytes"`
	// DroppedBefore counts captures of the same client the writing process
	// lost after its previous stored capture and before this one.
	DroppedBefore int64 `json:"dropped_before"`
}

// OutboundCaptureField is one header or query name with its values. Masked
// credential values read "[masked]".
//
//foundry:dto
type OutboundCaptureField struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// OutboundCaptureBody is one stored body. Content is base64 in JSON.
//
//foundry:dto
type OutboundCaptureBody struct {
	State OutboundBodyState `json:"state"`
	// Omission is set exactly when State is omitted.
	Omission value.Nullable[OutboundBodyOmission] `json:"omission"`
	// DeclaredBytes is the Content-Length; null when unknown.
	DeclaredBytes value.Nullable[int64] `json:"declared_bytes"`
	// TransferredBytes is what the transport sent (request) or the caller
	// read (response).
	TransferredBytes int64 `json:"transferred_bytes"`
	// Complete means the end of the body was observed: the transport sent
	// the whole request body, or the caller read the whole response. An
	// in-memory request body is stored whole even when it is false.
	Complete bool   `json:"complete"`
	Content  []byte `json:"content"`
}

// OutboundCaptureCause links a call to the inbound request that made it.
//
//foundry:dto
type OutboundCaptureCause struct {
	RequestID value.Nullable[attribution.RequestID] `json:"request_id"`
	// TraceID is the W3C trace ID as 32 lowercase hexadecimal characters.
	TraceID value.Nullable[string] `json:"trace_id"`
	// Route is the matched inbound route's declared name.
	Route value.Nullable[string] `json:"route"`
}

// OutboundCaptureDetail is one capture with its stored metadata and bodies.
//
//foundry:dto
type OutboundCaptureDetail struct {
	Capture          OutboundCapture        `json:"capture"`
	Scheme           string                 `json:"scheme"`
	Query            []OutboundCaptureField `json:"query"`
	RequestHeaders   []OutboundCaptureField `json:"request_headers"`
	RequestBody      OutboundCaptureBody    `json:"request_body"`
	Protocol         value.Nullable[string] `json:"protocol"`
	RemoteIP         value.Nullable[string] `json:"remote_ip"`
	ConnectionReused value.Nullable[bool]   `json:"connection_reused"`
	// ResponseHeaders are the headers the client received, with credential
	// names such as Set-Cookie masked.
	ResponseHeaders []OutboundCaptureField `json:"response_headers"`
	ResponseBody    OutboundCaptureBody    `json:"response_body"`
	Cause           OutboundCaptureCause   `json:"cause"`
	// Attempts are the operation's other stored attempts, oldest first.
	Attempts  []OutboundCapture         `json:"attempts"`
	Omissions []OutboundCaptureOmission `json:"omissions"`
}
