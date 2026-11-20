package webhookcapturestore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/capturelog"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// Table describes foundry_webhook_captures to the shared capture engine,
// which owns the writer, retention, the viewer read gate and the prune
// command. The row cap applies to the whole table.
func Table() capturelog.Table[Capture] {
	c := Captures
	return capturelog.Table[Capture]{
		Name: "foundry_webhook_captures", Label: "webhook capture", Lock: "foundry.webhook_capture",
		Query: c.Query, ID: c.ID, At: c.ReceivedAt, Insert: Insert, Size: Capture.Size,
	}
}

// Insert stores rows once. A replayed request ID is skipped, so retrying a
// batch after an unknown commit outcome cannot duplicate a capture.
func Insert(ctx context.Context, tx *database.Tx, captures []Capture) error {
	c := Captures
	drafts := make([][]orm.Assignment[Capture], 0, len(captures))
	for _, row := range captures {
		draft := []orm.Assignment[Capture]{
			orm.Set(c.ID, row.ID),
			orm.Set(c.ReceivedAt, row.ReceivedAt),
			orm.Set(c.Method, row.Method),
			orm.Set(c.Path, row.Path),
			nullableText(c.Route, row.Route),
			nullableText(c.ClientIP, row.ClientIP),
			orm.Set(c.Headers, row.Headers),
			orm.Set(c.QueryFields, row.Query),
			orm.Set(c.ContentType, row.ContentType),
			orm.Set(c.ContentEncoding, row.ContentEncoding),
			nullableNumber(c.DeclaredBytes, row.DeclaredBytes),
			nullableNumber(c.MeasuredBytes, row.MeasuredBytes),
			orm.Set(c.ObservedBytes, row.ObservedBytes),
			orm.Set(c.BodyMode, row.BodyMode),
			orm.Set(c.BodyState, row.BodyState),
			nullableText(c.BodyOmission, row.BodyOmission),
			orm.Set(c.Body, row.Body),
			nullableNumber(c.ResponseStatus, row.ResponseStatus),
			orm.Set(c.Outcome, row.Outcome),
			orm.Set(c.ResponseState, row.ResponseState),
			orm.Set(c.ResponseContentType, row.ResponseContentType),
			orm.Set(c.ResponseEncoding, row.ResponseEncoding),
			orm.Set(c.ResponseBytes, row.ResponseBytes),
			orm.Set(c.ResponseHead, row.ResponseHead),
			orm.Set(c.ResponseHeaders, row.ResponseHeaders),
			orm.Set(c.DurationMS, row.DurationMS),
			orm.Set(c.Omissions, row.Omissions),
			orm.Set(c.DroppedBefore, row.DroppedBefore),
		}
		drafts = append(drafts, draft)
	}
	if len(drafts) == 0 {
		return nil
	}
	_, err := orm.UpsertMany(ctx, tx, c.Query(), orm.OnConflict(orm.ConflictKey(c.ID)).DoNothing(), drafts)
	return err
}

func nullableText(column orm.NullableText[Capture, string], v interface{ Get() (string, bool) }) orm.Assignment[Capture] {
	if text, present := v.Get(); present {
		return orm.Set(column, text)
	}
	return orm.SetNull(column)
}

func nullableNumber[V orm.NumberValue](column orm.NullableNumber[Capture, V], v interface{ Get() (V, bool) }) orm.Assignment[Capture] {
	if number, present := v.Get(); present {
		return orm.Set(column, number)
	}
	return orm.SetNull(column)
}

// Find loads one capture still inside retention, or database.NotFound.
func Find(ctx context.Context, tx *database.Tx, id model.ID[Capture], retained temporal.DateTime) (Capture, error) {
	return orm.Require(ctx, tx, Captures.Query().Where(orm.Eq(Captures.ID, id), orm.Gte(Captures.ReceivedAt, retained)))
}

// Page reads one newest-first keyset page of the narrow projection. q holds
// every filter; the cursor adds only its position.
func Page(ctx context.Context, tx *database.Tx, q orm.Query[Capture], request orm.CursorRequest[Summary]) (orm.CursorPage[Summary], error) {
	cursor := rows.CursorFor(rows.RecordOf(Summaries(q)))
	at := cursor.Scope()
	id := rows.At(at, summaryColumns.ID)
	ordered := cursor.OrderBy(expr.Desc(rows.At(at, summaryColumns.ReceivedAt)), expr.Desc(id)).UniqueBy(expr.Group(id))
	return ordered.Paginate(ctx, tx, request)
}
