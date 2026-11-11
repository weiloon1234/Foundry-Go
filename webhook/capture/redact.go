package capture

import (
	"github.com/weiloon1234/Foundry-Go/internal/capturedisclosure"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
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

func (p policy) disclosure() capturedisclosure.Policy {
	return capturedisclosure.Policy{ValueBytes: p.valueBytes, BodyBytes: p.bodyBytes}
}

func requestCredential(name string) bool { return capturedisclosure.CredentialName(name, nil) }
func queryCredential(name string) bool   { return capturedisclosure.CredentialQueryName(name, nil) }
func noCredential(string) bool           { return false }

// redact turns one kernel snapshot into the stored row without I/O, through
// the shared disclosure owner. Input it cannot represent safely is omitted
// with a reason, never stored raw. Request header, query and body masking use
// the shared credential rule in both modes, including the query of a URL-valued
// request header such as Referer; response headers stay whole.
// The writer sets DroppedBefore from the record that carried the snapshot.
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
	query, queryTruncated, queryValid := capturedisclosure.Fields(m.RawQuery, m.QueryTruncated, p.disclosure(), queryCredential)
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
		ID: id, ReceivedAt: received, Method: capturedisclosure.TextLimit(m.Method, 32), Path: capturedisclosure.TextLimit(m.EscapedPath, 4<<10),
		ContentType: capturedisclosure.TextLimit(m.ContentType, 256), ContentEncoding: capturedisclosure.TextLimit(m.ContentEncoding, 256),
		DeclaredBytes: m.DeclaredBytes, MeasuredBytes: s.Body.MeasuredBytes, ObservedBytes: s.Body.ObservedBytes,
		BodyMode: string(mode), BodyState: string(state), Body: capturedisclosure.Bytes(body),
		Outcome: outcome(c.Result), ResponseState: string(responseState(c)),
		ResponseContentType: capturedisclosure.TextLimit(c.ResponseContentType, 256), ResponseEncoding: capturedisclosure.TextLimit(c.ResponseEncoding, 256),
		ResponseBytes: c.ResponseBytes, ResponseHead: capturedisclosure.Bytes(c.ResponseHead),
		DurationMS: max(0, c.Duration.Milliseconds()),
	}
	if omission != "" {
		row.BodyOmission = value.Of(string(omission))
	}
	if c.Route != "" {
		row.Route = value.Of(capturedisclosure.TextLimit(c.Route, 256))
	}
	if m.ClientIP.IsValid() {
		row.ClientIP = value.Of(m.ClientIP.WithZone("").String())
	}
	if status, present := c.Status.Get(); present {
		row.ResponseStatus = value.Of(int32(status))
	}
	if row.Headers, err = value.NewJSON(capturedisclosure.Headers(m.Headers, requestCredential, queryCredential)); err != nil {
		return store.Capture{}, err
	}
	if row.ResponseHeaders, err = value.NewJSON(capturedisclosure.Headers(c.ResponseHeaders, noCredential, noCredential)); err != nil {
		return store.Capture{}, err
	}
	if row.Query, err = value.NewJSON(capturedisclosure.NonNil(query)); err != nil {
		return store.Capture{}, err
	}
	names := make([]string, len(omissions))
	for i, omission := range omissions {
		names[i] = string(omission)
	}
	row.Omissions, err = value.NewJSON(names)
	return row, err
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
	media, reason := capturedisclosure.Inspectable(m.ContentType, m.ContentEncoding)
	if reason != capturedisclosure.NoReason {
		return BodyOmitted, omission(reason), nil
	}
	out, reason := capturedisclosure.Redact(b.Prefix, media, p.disclosure())
	if reason != capturedisclosure.NoReason {
		return BodyOmitted, omission(reason), nil
	}
	return bodyStates[media.Format], "", out
}

var bodyStates = [...]CaptureBodyState{capturedisclosure.JSON: BodyJSON, capturedisclosure.Form: BodyForm, capturedisclosure.Multipart: BodyMultipart}

func omission(reason capturedisclosure.Reason) CaptureBodyOmission {
	switch reason {
	case capturedisclosure.ContentEncoded:
		return OmittedContentEncoded
	case capturedisclosure.UnsupportedMediaType:
		return OmittedUnsupportedMediaType
	case capturedisclosure.RedactionLimit:
		return OmittedRedactionLimit
	default:
		return OmittedMalformed
	}
}
