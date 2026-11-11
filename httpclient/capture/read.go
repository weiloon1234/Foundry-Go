package capture

import (
	"context"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/capturedisclosure"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

const maxWindow = 30 * 24 * time.Hour

var (
	errWindow = invalid("outbound capture list requires from and a window of at most 30 days")
	errStatus = invalid("outbound capture status filter must be between 100 and 599")
)

// list reads one newest-first page inside a bounded window. Rows past
// retention are dropped from the page even before pruning removes them.
func (s *Store) list(ctx context.Context, filters OutboundCaptureFilters, request orm.CursorRequest[store.Summary]) (pagination.CursorResult[store.Summary, OutboundCapture], error) {
	var none pagination.CursorResult[store.Summary, OutboundCapture]
	until, bounded := filters.Until.Get()
	end := s.clock.Now()
	if bounded {
		end = until.UTC()
	}
	from := filters.From.UTC()
	if filters.From.IsZero() || !end.After(from) || end.Sub(from) > maxWindow {
		return none, http.BadRequest.WithCause(errWindow)
	}
	c := store.Captures
	q := c.Query().Where(orm.TimeGte(c.StartedAt, filters.From))
	if bounded {
		q = q.Where(orm.TimeLt(c.StartedAt, until))
	}
	if client, set := filters.Client.Get(); set {
		q = q.Where(orm.TextEq(c.Client, string(client)))
	}
	if host, set := filters.Host.Get(); set {
		q = q.Where(orm.TextEq(c.Host, host))
	}
	if method, set := filters.Method.Get(); set {
		q = q.Where(orm.TextEq(c.Method, method))
	}
	if status, set := filters.Status.Get(); set {
		if status < 100 || status > 599 {
			return none, http.BadRequest.WithCause(errStatus)
		}
		q = q.Where(orm.NullableNumberEq(c.Status, int16(status)))
	}
	if result, set := filters.Result.Get(); set {
		q = q.Where(orm.TextEq(c.Result, string(result)))
	}
	if problems, set := filters.Problems.Get(); set && problems {
		q = q.Where(orm.Or(orm.TextNe(c.Result, string(ResultCompleted)), orm.NullableNumberGte(c.Status, int16(400))))
	}
	if id, set := filters.RequestID.Get(); set {
		q = q.Where(orm.NullableTextEq(c.RequestID, string(id)))
	}
	if operation, set := filters.Operation.Get(); set {
		q = q.Where(orm.ColumnEq(c.OperationID, model.IDFromBytes[store.Operation](operation.Bytes())))
	}
	cutoff, err := s.log.Cutoff()
	if err != nil {
		return none, err
	}
	var page orm.CursorPage[store.Summary]
	err = s.log.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		page, err = store.Page(ctx, tx, q, request)
		return err
	})
	if err != nil {
		return none, err
	}
	// Newest first: expired rows only ever form the page's tail, and every
	// later page would be expired too.
	for i, item := range page.Items {
		if item.StartedAt.UTC().Before(cutoff.UTC()) {
			page.Items, page.Next = page.Items[:i], value.Optional[orm.Cursor[store.Summary]]{}
			if i == 0 {
				page.Previous = value.Optional[orm.Cursor[store.Summary]]{}
			}
			break
		}
	}
	return pagination.MapCursorPage(ctx, page, present)
}

// find loads one capture and its operation's other attempts. An unknown or
// expired ID is not found.
func (s *Store) find(ctx context.Context, id ExchangeID) (OutboundCaptureDetail, error) {
	cutoff, err := s.log.Cutoff()
	if err != nil {
		return OutboundCaptureDetail{}, err
	}
	var row store.Capture
	var siblings []store.Summary
	err = s.log.Read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		if row, err = store.Find(ctx, tx, model.IDFromBytes[store.Capture](id.Bytes()), cutoff); err != nil {
			return err
		}
		siblings, err = store.Siblings(ctx, tx, row.OperationID, cutoff)
		return err
	})
	if errors.Is(err, database.NotFound) {
		return OutboundCaptureDetail{}, http.NotFound.WithCause(err)
	}
	if err != nil {
		return OutboundCaptureDetail{}, err
	}
	return presentDetail(row, siblings)
}

func present(row store.Summary) (OutboundCapture, error) {
	result := OutboundCapture{
		ID: model.IDFromBytes[Exchange](row.ID.Bytes()), OperationID: model.IDFromBytes[Operation](row.OperationID.Bytes()),
		Attempt: int32(row.Attempt), Retried: row.Retried, StartedAt: row.StartedAt, Client: httpclient.Name(row.Client),
		Method: row.Method, Host: row.Host, Path: row.Path, Result: OutboundResult(row.Result), HeadersMS: row.HeadersMS,
		DurationMS: row.DurationMS, Mode: OutboundCaptureMode(row.BodyMode), RequestSentBytes: row.RequestSentBytes,
		ResponseReadBytes: row.ResponseReadBytes, DroppedBefore: row.DroppedBefore,
	}
	if status, set := row.Status.Get(); set {
		result.Status = value.Of(int32(status))
	}
	if id, set := row.RequestID.Get(); set {
		result.RequestID = value.Of(attribution.RequestID(id))
	}
	for _, err := range []error{result.Result.Validate(), result.Mode.Validate()} {
		if err != nil {
			return OutboundCapture{}, err
		}
	}
	return result, nil
}

func presentDetail(row store.Capture, siblings []store.Summary) (OutboundCaptureDetail, error) {
	head, err := present(store.Summary{
		ID: row.ID, OperationID: row.OperationID, Attempt: row.Attempt, Retried: row.Retried, StartedAt: row.StartedAt,
		Client: row.Client, Method: row.Method, Host: row.Host, Path: row.Path, Status: row.Status, Result: row.Result,
		HeadersMS: row.HeadersMS, DurationMS: row.DurationMS, RequestID: row.RequestID, BodyMode: row.BodyMode,
		RequestSentBytes: row.RequestSentBytes, ResponseReadBytes: row.ResponseReadBytes, DroppedBefore: row.DroppedBefore,
	})
	if err != nil {
		return OutboundCaptureDetail{}, err
	}
	detail := OutboundCaptureDetail{Capture: head, Scheme: row.Scheme, Protocol: row.Protocol, RemoteIP: row.RemoteIP, ConnectionReused: row.ConnectionReused, Attempts: []OutboundCapture{}}
	for _, item := range []struct {
		stored value.JSON[[]httpcapture.Field]
		into   *[]OutboundCaptureField
	}{{row.Query, &detail.Query}, {row.RequestHeaders, &detail.RequestHeaders}, {row.ResponseHeaders, &detail.ResponseHeaders}} {
		if *item.into, err = fields(item.stored); err != nil {
			return OutboundCaptureDetail{}, err
		}
	}
	if detail.RequestBody, err = body(row.RequestBodyState, row.RequestBodyOmission, row.RequestDeclaredBytes, row.RequestSentBytes, row.RequestBodyComplete, row.RequestBody); err != nil {
		return OutboundCaptureDetail{}, err
	}
	if detail.ResponseBody, err = body(row.ResponseBodyState, row.ResponseBodyOmission, row.ResponseDeclaredBytes, row.ResponseReadBytes, row.ResponseBodyComplete, row.ResponseBody); err != nil {
		return OutboundCaptureDetail{}, err
	}
	if id, set := row.RequestID.Get(); set {
		detail.Cause.RequestID = value.Of(attribution.RequestID(id))
	}
	detail.Cause.TraceID, detail.Cause.Route = row.TraceID, row.Route
	names, err := row.Omissions.Decode()
	if err != nil {
		return OutboundCaptureDetail{}, err
	}
	detail.Omissions = make([]OutboundCaptureOmission, len(names))
	for i, name := range names {
		if detail.Omissions[i], err = ParseOutboundCaptureOmission(name); err != nil {
			return OutboundCaptureDetail{}, err
		}
	}
	for _, sibling := range siblings {
		if sibling.ID == row.ID {
			continue
		}
		attempt, err := present(sibling)
		if err != nil {
			return OutboundCaptureDetail{}, err
		}
		detail.Attempts = append(detail.Attempts, attempt)
	}
	return detail, nil
}

func body(state string, omission value.Nullable[string], declared value.Nullable[int64], transferred int64, complete bool, content []byte) (OutboundCaptureBody, error) {
	result := OutboundCaptureBody{State: OutboundBodyState(state), DeclaredBytes: declared, TransferredBytes: transferred, Complete: complete, Content: capturedisclosure.NonNil(content)}
	if err := result.State.Validate(); err != nil {
		return OutboundCaptureBody{}, err
	}
	if reason, set := omission.Get(); set {
		parsed, err := ParseOutboundBodyOmission(reason)
		if err != nil {
			return OutboundCaptureBody{}, err
		}
		result.Omission = value.Of(parsed)
	}
	return result, nil
}

func fields(stored value.JSON[[]httpcapture.Field]) ([]OutboundCaptureField, error) {
	decoded, err := stored.Decode()
	if err != nil {
		return nil, err
	}
	result := make([]OutboundCaptureField, len(decoded))
	for i, field := range decoded {
		result[i] = OutboundCaptureField{Name: field.Name, Values: capturedisclosure.NonNil(field.Values)}
	}
	return result, nil
}
