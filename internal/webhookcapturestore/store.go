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
			orm.KeySet(c.ID, row.ID),
			orm.TimeSet(c.ReceivedAt, row.ReceivedAt),
			orm.TextSet(c.Method, row.Method),
			orm.TextSet(c.Path, row.Path),
			nullableText(c.Route, row.Route),
			nullableText(c.ClientIP, row.ClientIP),
			orm.JSONSet(c.Headers, row.Headers),
			orm.JSONSet(c.QueryFields, row.Query),
			orm.TextSet(c.ContentType, row.ContentType),
			orm.TextSet(c.ContentEncoding, row.ContentEncoding),
			nullableNumber(c.DeclaredBytes, row.DeclaredBytes),
			nullableNumber(c.MeasuredBytes, row.MeasuredBytes),
			orm.NumberSet(c.ObservedBytes, row.ObservedBytes),
			orm.TextSet(c.BodyMode, row.BodyMode),
			orm.TextSet(c.BodyState, row.BodyState),
			nullableText(c.BodyOmission, row.BodyOmission),
			orm.ColumnSet(c.Body, row.Body),
			nullableNumber(c.ResponseStatus, row.ResponseStatus),
			orm.TextSet(c.Outcome, row.Outcome),
			orm.TextSet(c.ResponseState, row.ResponseState),
			orm.TextSet(c.ResponseContentType, row.ResponseContentType),
			orm.TextSet(c.ResponseEncoding, row.ResponseEncoding),
			orm.NumberSet(c.ResponseBytes, row.ResponseBytes),
			orm.ColumnSet(c.ResponseHead, row.ResponseHead),
			orm.JSONSet(c.ResponseHeaders, row.ResponseHeaders),
			orm.NumberSet(c.DurationMS, row.DurationMS),
			orm.JSONSet(c.Omissions, row.Omissions),
			orm.NumberSet(c.DroppedBefore, row.DroppedBefore),
		}
		drafts = append(drafts, draft)
	}
	if len(drafts) == 0 {
		return nil
	}
	_, err := orm.UpsertMany(ctx, tx, c.Query(), orm.OnConflict(orm.ConflictKey(orm.KeyExpr(c.ID))).DoNothing(), drafts)
	return err
}

func nullableText(column orm.NullableText[Capture, string], v interface{ Get() (string, bool) }) orm.Assignment[Capture] {
	if text, present := v.Get(); present {
		return orm.NullableTextSet(column, text)
	}
	return orm.NullableTextSetNull(column)
}

func nullableNumber[V orm.NumberValue](column orm.NullableNumber[Capture, V], v interface{ Get() (V, bool) }) orm.Assignment[Capture] {
	if number, present := v.Get(); present {
		return orm.NullableNumberSet(column, number)
	}
	return orm.NullableNumberSetNull(column)
}

// Find loads one capture still inside retention, or database.NotFound.
func Find(ctx context.Context, tx *database.Tx, id model.ID[Capture], retained temporal.DateTime) (Capture, error) {
	return orm.Require(ctx, tx, Captures.Query().Where(orm.KeyEq(Captures.ID, id), orm.TimeGte(Captures.ReceivedAt, retained)))
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
