package capture

import (
	"github.com/weiloon1234/Foundry-Go/internal/capturedisclosure"
	"github.com/weiloon1234/Foundry-Go/internal/capturelog"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// results is the one exhaustive mapping from the recorder's closed outcome.
var results = [...]OutboundResult{
	httpcapture.ResultCompleted:          ResultCompleted,
	httpcapture.ResultConsumerFailed:     ResultConsumerFailed,
	httpcapture.ResultResponseBodyFailed: ResultResponseBodyFailed,
	httpcapture.ResultResponseTooLarge:   ResultResponseTooLarge,
	httpcapture.ResultRequestBodyFailed:  ResultRequestBodyFailed,
	httpcapture.ResultTransportFailed:    ResultTransportFailed,
	httpcapture.ResultTimedOut:           ResultTimedOut,
	httpcapture.ResultCanceled:           ResultCanceled,
	httpcapture.ResultDestinationDenied:  ResultDestinationDenied,
	httpcapture.ResultOverloaded:         ResultOverloaded,
}

var skipped = [...]OutboundBodyOmission{
	httpcapture.SkipNotSent:  OmittedNotSent,
	httpcapture.SkipMedia:    OmittedUnsupportedMediaType,
	httpcapture.SkipEncoded:  OmittedContentEncoded,
	httpcapture.SkipDeclined: OmittedDeclined,
	httpcapture.SkipMemory:   OmittedCaptureMemory,
}

var redactedStates = [...]OutboundBodyState{capturedisclosure.JSON: BodyJSON, capturedisclosure.Form: BodyForm, capturedisclosure.Multipart: BodyMultipart}

// redact turns one attempt snapshot into the stored row without I/O, through
// the shared disclosure owner. Credential header, query and body field names
// are masked in every mode; input redaction cannot represent safely is
// omitted with a reason, never stored raw.
func redact(e httpcapture.Exchange, dropped uint64, p capturedisclosure.Policy) (store.Capture, error) {
	started, err := temporal.NewDateTime(e.StartedAt)
	if err != nil {
		return store.Capture{}, err
	}
	if int(e.Result) >= len(results) {
		return store.Capture{}, invalid("unknown outbound capture result")
	}
	mode := Redacted
	if e.Full {
		mode = Full
	}
	path := capturedisclosure.TextLimit(e.EscapedPath, 4<<10)
	if path == "" {
		path = "/"
	}
	row := store.Capture{
		ID: model.IDFromBytes[store.Capture](e.ID), OperationID: model.IDFromBytes[store.Operation](e.Operation),
		Attempt: int16(e.Attempt), Retried: e.Retried, Client: capturedisclosure.TextLimit(e.Client, 128), StartedAt: started,
		DurationMS: max(0, e.Duration.Milliseconds()), Result: string(results[e.Result]),
		Method: capturedisclosure.TextLimit(e.Method, 32), Scheme: e.Scheme, Host: capturedisclosure.TextLimit(e.Host, 300), Path: path,
		BodyMode: string(mode), DroppedBefore: capturelog.DroppedBefore(dropped),
	}
	if after, present := e.HeadersAfter.Get(); present {
		row.HeadersMS = value.Of(max(0, after.Milliseconds()))
	}
	if id := e.Cause.RequestID; id != "" {
		row.RequestID = value.Of(capturedisclosure.TextLimit(string(id), 128))
	}
	if !e.Cause.Trace.IsZero() {
		row.TraceID = value.Of(e.Cause.Trace.String())
	}
	if e.Cause.Route != "" {
		row.Route = value.Of(capturedisclosure.TextLimit(e.Cause.Route, 256))
	}
	if status, present := e.Status.Get(); present {
		row.Status = value.Of(int16(status))
	}
	if e.Protocol != "" {
		row.Protocol = value.Of(capturedisclosure.TextLimit(e.Protocol, 16))
	}
	if e.RemoteIP.IsValid() {
		row.RemoteIP = value.Of(e.RemoteIP.WithZone("").String())
	}
	row.ConnectionReused = e.Reused
	credential := func(name string) bool { return capturedisclosure.CredentialName(name, p.Names) }
	query, queryCut, queryValid := capturedisclosure.Fields(e.RawQuery, e.QueryCut, p, func(name string) bool { return capturedisclosure.CredentialQueryName(name, p.Names) })
	if !queryValid {
		query = nil
	}
	if row.Query, err = value.NewJSON(capturedisclosure.NonNil(query)); err != nil {
		return store.Capture{}, err
	}
	if row.RequestHeaders, err = value.NewJSON(capturedisclosure.Headers(e.RequestHeaders, credential)); err != nil {
		return store.Capture{}, err
	}
	if row.ResponseHeaders, err = value.NewJSON(capturedisclosure.Headers(e.ResponseHeaders, credential)); err != nil {
		return store.Capture{}, err
	}
	var omissions []string
	for _, omission := range []struct {
		present bool
		name    OutboundCaptureOmission
	}{{e.PathCut, PathTruncated}, {queryCut, QueryTruncated}, {!queryValid, QueryMalformed}, {e.RequestHeadersCut, RequestHeadersTruncated}, {e.ResponseHeadersCut, ResponseHeadersTruncated}} {
		if omission.present {
			omissions = append(omissions, string(omission.name))
		}
	}
	if row.Omissions, err = value.NewJSON(capturedisclosure.NonNil(omissions)); err != nil {
		return store.Capture{}, err
	}
	state, omission, body := storedBody(e.Request, e.Full, p)
	row.RequestBodyState, row.RequestBodyOmission, row.RequestBody = string(state), nullableOmission(omission), capturedisclosure.Bytes(body)
	row.RequestDeclaredBytes, row.RequestSentBytes, row.RequestBodyComplete = e.Request.Declared, e.Request.Transferred, e.Request.Complete
	if e.Status.IsNull() {
		state, omission, body = BodyOmitted, OmittedNoResponse, nil
	} else {
		state, omission, body = storedBody(e.Response, e.Full, p)
	}
	row.ResponseBodyState, row.ResponseBodyOmission, row.ResponseBody = string(state), nullableOmission(omission), capturedisclosure.Bytes(body)
	row.ResponseDeclaredBytes, row.ResponseReadBytes, row.ResponseBodyComplete = e.Response.Declared, e.Response.Transferred, e.Response.Complete
	return row, nil
}

func nullableOmission(omission OutboundBodyOmission) value.Nullable[string] {
	if omission == "" {
		return value.Nullable[string]{}
	}
	return value.Of(string(omission))
}

// storedBody decides one body's stored form. Completeness comes only from an
// observed end of stream: a partial body is an exact prefix in full mode and
// omitted in redacted mode, because a truncated JSON prefix that happens to
// parse would misstate the payload.
func storedBody(b httpcapture.BodyCapture, full bool, p capturedisclosure.Policy) (OutboundBodyState, OutboundBodyOmission, []byte) {
	// A body observed to end without a byte is empty whatever its media type,
	// as with a 204 that carries no Content-Type.
	declared, known := b.Declared.Get()
	if (b.Skip == httpcapture.SkipMedia || b.Skip == httpcapture.SkipEncoded) && b.Complete && b.Transferred == 0 && (!known || declared == 0) {
		return BodyEmpty, "", nil
	}
	if b.Skip != httpcapture.Kept {
		return BodyOmitted, skipped[b.Skip], nil
	}
	if b.Over || !b.Complete {
		switch {
		case full && len(b.Bytes) > 0:
			return BodyExactPrefix, "", b.Bytes
		case b.Over:
			return BodyOmitted, OmittedOverCaptureLimit, nil
		default:
			return BodyOmitted, OmittedIncomplete, nil
		}
	}
	if len(b.Bytes) == 0 {
		return BodyEmpty, "", nil
	}
	if full {
		return BodyExact, "", b.Bytes
	}
	media, reason := capturedisclosure.Inspectable(b.ContentType, b.ContentEncoding)
	if reason == capturedisclosure.NoReason {
		var out []byte
		if out, reason = capturedisclosure.Redact(b.Bytes, media, p); reason == capturedisclosure.NoReason {
			return redactedStates[media.Format], "", out
		}
	}
	switch reason {
	case capturedisclosure.ContentEncoded:
		return BodyOmitted, OmittedContentEncoded, nil
	case capturedisclosure.UnsupportedMediaType:
		return BodyOmitted, OmittedUnsupportedMediaType, nil
	case capturedisclosure.RedactionLimit:
		return BodyOmitted, OmittedRedactionLimit, nil
	default:
		return BodyOmitted, OmittedMalformed, nil
	}
}
