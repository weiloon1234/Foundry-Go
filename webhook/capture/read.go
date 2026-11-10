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
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

var errWindow = invalid("webhook capture list requires from and a window of at most 30 days")

const (
	maxReads    = 2
	readTimeout = 2 * time.Second
	maxWindow   = 30 * 24 * time.Hour
)

// read admits at most maxReads concurrent viewer reads, each bounded by
// readTimeout including the wait for a slot.
func (s *Store) read(ctx context.Context, fn func(context.Context, *database.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	select {
	case s.reads <- struct{}{}:
	case <-ctx.Done():
		return http.Unavailable.WithCause(ctx.Err())
	}
	defer func() { <-s.reads }()
	return s.scope.Transaction(ctx, true, func(tx *database.Tx) error { return fn(ctx, tx) })
}

// list reads one newest-first page inside a bounded window. Rows past
// retention are dropped from the page even before pruning removes them.
func (s *Store) list(ctx context.Context, filters CapturedRequestFilters, request orm.CursorRequest[store.Summary]) (pagination.CursorResult[store.Summary, CapturedRequest], error) {
	now := s.clock.Now()
	until, bounded := filters.Until.Get()
	end := now
	if bounded {
		end = until.UTC()
	}
	from := filters.From.UTC()
	if filters.From.IsZero() || !end.After(from) || end.Sub(from) > maxWindow {
		return pagination.CursorResult[store.Summary, CapturedRequest]{}, http.BadRequest.WithCause(errWindow)
	}
	c := store.Captures
	q := c.Query().Where(orm.TimeGte(c.ReceivedAt, filters.From))
	if bounded {
		q = q.Where(orm.TimeLt(c.ReceivedAt, until))
	}
	if method, set := filters.Method.Get(); set {
		q = q.Where(orm.TextEq(c.Method, method))
	}
	if prefix, set := filters.PathPrefix.Get(); set {
		q = q.Where(orm.TextStartsWith(c.Path, prefix))
	}
	if status, set := filters.Status.Get(); set {
		q = q.Where(orm.NullableNumberEq(c.ResponseStatus, status))
	}
	if mode, set := filters.Mode.Get(); set {
		q = q.Where(orm.TextEq(c.BodyMode, string(mode)))
	}
	cutoff, err := s.retentionCutoff()
	if err != nil {
		return pagination.CursorResult[store.Summary, CapturedRequest]{}, err
	}
	var page orm.CursorPage[store.Summary]
	err = s.read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		page, err = store.Page(ctx, tx, q, request)
		return err
	})
	if err != nil {
		return pagination.CursorResult[store.Summary, CapturedRequest]{}, err
	}
	// Newest first: expired rows only ever form the page's tail, and every
	// later page would be expired too.
	for i, item := range page.Items {
		if item.ReceivedAt.UTC().Before(cutoff.UTC()) {
			page.Items, page.Next = page.Items[:i], value.Optional[orm.Cursor[store.Summary]]{}
			if i == 0 {
				page.Previous = value.Optional[orm.Cursor[store.Summary]]{}
			}
			break
		}
	}
	return pagination.MapCursorPage(ctx, page, present)
}

// find loads one capture by request ID. A malformed, unknown or expired ID
// is not found.
func (s *Store) find(ctx context.Context, request attribution.RequestID) (CapturedRequestDetail, error) {
	id, err := model.ParseID[store.Capture](string(request))
	if err != nil {
		return CapturedRequestDetail{}, http.NotFound.WithCause(err)
	}
	cutoff, err := s.retentionCutoff()
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	var row store.Capture
	err = s.read(ctx, func(ctx context.Context, tx *database.Tx) error {
		var err error
		row, err = store.Find(ctx, tx, id, cutoff)
		return err
	})
	if errors.Is(err, database.NotFound) {
		return CapturedRequestDetail{}, http.NotFound.WithCause(err)
	}
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	return presentDetail(row)
}

func present(row store.Summary) (CapturedRequest, error) {
	result := CapturedRequest{
		RequestID: attribution.RequestID(row.ID.String()), ReceivedAt: row.ReceivedAt, Method: row.Method, Path: row.Path,
		Route: row.Route, ClientIP: row.ClientIP, Status: row.ResponseStatus, Outcome: row.Outcome,
		ResponseState: CaptureResponseState(row.ResponseState), DurationMS: row.DurationMS,
		Mode: CaptureMode(row.BodyMode), BodyState: CaptureBodyState(row.BodyState),
		DeclaredBytes: row.DeclaredBytes, MeasuredBytes: row.MeasuredBytes, ObservedBytes: row.ObservedBytes, DroppedBefore: row.DroppedBefore,
	}
	if omission, set := row.BodyOmission.Get(); set {
		result.BodyOmission = value.Of(CaptureBodyOmission(omission))
		if err := CaptureBodyOmission(omission).Validate(); err != nil {
			return CapturedRequest{}, err
		}
	}
	for _, err := range []error{result.ResponseState.Validate(), result.Mode.Validate(), result.BodyState.Validate()} {
		if err != nil {
			return CapturedRequest{}, err
		}
	}
	return result, nil
}

func presentDetail(row store.Capture) (CapturedRequestDetail, error) {
	summary, err := present(store.Summary{
		ID: row.ID, ReceivedAt: row.ReceivedAt, Method: row.Method, Path: row.Path, Route: row.Route, ClientIP: row.ClientIP,
		ResponseStatus: row.ResponseStatus, Outcome: row.Outcome, ResponseState: row.ResponseState, DurationMS: row.DurationMS,
		BodyMode: row.BodyMode, BodyState: row.BodyState, BodyOmission: row.BodyOmission,
		DeclaredBytes: row.DeclaredBytes, MeasuredBytes: row.MeasuredBytes, ObservedBytes: row.ObservedBytes, DroppedBefore: row.DroppedBefore,
	})
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	headers, err := fields(row.Headers)
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	query, err := fields(row.Query)
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	responseHeaders, err := fields(row.ResponseHeaders)
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	names, err := row.Omissions.Decode()
	if err != nil {
		return CapturedRequestDetail{}, err
	}
	omissions := make([]CaptureOmission, len(names))
	for i, name := range names {
		if omissions[i], err = ParseCaptureOmission(name); err != nil {
			return CapturedRequestDetail{}, err
		}
	}
	return CapturedRequestDetail{
		Request: summary, Headers: headers, Query: query, ContentType: row.ContentType, ContentEncoding: row.ContentEncoding,
		Body: nonNil(row.Body), ResponseContentType: row.ResponseContentType, ResponseEncoding: row.ResponseEncoding,
		ResponseBytes: row.ResponseBytes, ResponseHead: nonNil(row.ResponseHead), ResponseHeaders: responseHeaders, Omissions: omissions,
	}, nil
}

func fields(stored value.JSON[[]store.Field]) ([]CapturedField, error) {
	decoded, err := stored.Decode()
	if err != nil {
		return nil, err
	}
	result := make([]CapturedField, len(decoded))
	for i, field := range decoded {
		result[i] = CapturedField{Name: field.Name, Values: nonNil(field.Values)}
	}
	return result, nil
}
