package outboundcapturestore

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

// MaxSiblings bounds one operation's rows: attempt 0 or attempts 1 to 10.
const MaxSiblings = 11

// Table describes foundry_outbound_captures to the shared capture engine.
// The row cap applies per client, so a chatty client never evicts another
// client's rows.
func Table() capturelog.Table[Capture] {
	c := Captures
	client := c.Client
	return capturelog.Table[Capture]{
		Name: "foundry_outbound_captures", Label: "outbound capture", Lock: "foundry.outbound_capture",
		Query: c.Query, ID: c.ID, At: c.StartedAt, Partition: &client, Insert: Insert, Size: Capture.Size,
	}
}

// Insert stores rows once. A replayed ID is skipped, so retrying a batch after
// an unknown commit outcome cannot duplicate a capture.
func Insert(ctx context.Context, tx *database.Tx, captures []Capture) error {
	c := Captures
	drafts := make([][]orm.Assignment[Capture], 0, len(captures))
	for _, row := range captures {
		drafts = append(drafts, []orm.Assignment[Capture]{
			orm.KeySet(c.ID, row.ID),
			orm.ColumnSet(c.OperationID, row.OperationID),
			orm.NumberSet(c.Attempt, row.Attempt),
			orm.ColumnSet(c.Retried, row.Retried),
			orm.TextSet(c.Client, row.Client),
			orm.TimeSet(c.StartedAt, row.StartedAt),
			nullableNumber(c.HeadersMS, row.HeadersMS),
			orm.NumberSet(c.DurationMS, row.DurationMS),
			orm.TextSet(c.Result, row.Result),
			nullableText(c.RequestID, row.RequestID),
			nullableText(c.TraceID, row.TraceID),
			nullableText(c.Route, row.Route),
			orm.TextSet(c.Method, row.Method),
			orm.TextSet(c.Scheme, row.Scheme),
			orm.TextSet(c.Host, row.Host),
			orm.TextSet(c.Path, row.Path),
			orm.JSONSet(c.QueryFields, row.Query),
			orm.JSONSet(c.RequestHeaders, row.RequestHeaders),
			orm.TextSet(c.BodyMode, row.BodyMode),
			orm.TextSet(c.RequestBodyState, row.RequestBodyState),
			nullableText(c.RequestBodyOmission, row.RequestBodyOmission),
			orm.ColumnSet(c.RequestBody, row.RequestBody),
			nullableNumber(c.RequestDeclaredBytes, row.RequestDeclaredBytes),
			orm.NumberSet(c.RequestSentBytes, row.RequestSentBytes),
			orm.ColumnSet(c.RequestBodyComplete, row.RequestBodyComplete),
			nullableNumber(c.Status, row.Status),
			nullableText(c.Protocol, row.Protocol),
			nullableText(c.RemoteIP, row.RemoteIP),
			nullableBool(c.ConnectionReused, row.ConnectionReused),
			orm.JSONSet(c.ResponseHeaders, row.ResponseHeaders),
			orm.TextSet(c.ResponseBodyState, row.ResponseBodyState),
			nullableText(c.ResponseBodyOmission, row.ResponseBodyOmission),
			orm.ColumnSet(c.ResponseBody, row.ResponseBody),
			nullableNumber(c.ResponseDeclaredBytes, row.ResponseDeclaredBytes),
			orm.NumberSet(c.ResponseReadBytes, row.ResponseReadBytes),
			orm.ColumnSet(c.ResponseBodyComplete, row.ResponseBodyComplete),
			orm.JSONSet(c.Omissions, row.Omissions),
			orm.NumberSet(c.DroppedBefore, row.DroppedBefore),
		})
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

func nullableBool(column orm.NullableColumn[Capture, bool], v interface{ Get() (bool, bool) }) orm.Assignment[Capture] {
	if flag, present := v.Get(); present {
		return orm.NullableColumnSet(column, flag)
	}
	return orm.NullableColumnSetNull(column)
}

// Find loads one capture still inside retention, or database.NotFound.
func Find(ctx context.Context, tx *database.Tx, id model.ID[Capture], retained temporal.DateTime) (Capture, error) {
	return orm.Require(ctx, tx, Captures.Query().Where(orm.KeyEq(Captures.ID, id), orm.TimeGte(Captures.StartedAt, retained)))
}

// Page reads one newest-first keyset page of the narrow projection. q holds
// every filter; the cursor adds only its position.
func Page(ctx context.Context, tx *database.Tx, q orm.Query[Capture], request orm.CursorRequest[Summary]) (orm.CursorPage[Summary], error) {
	cursor := rows.CursorFor(rows.RecordOf(Summaries(q)))
	at := cursor.Scope()
	id := rows.At(at, summaryColumns.ID)
	ordered := cursor.OrderBy(expr.Desc(rows.At(at, summaryColumns.StartedAt)), expr.Desc(id)).UniqueBy(expr.Group(id))
	return ordered.Paginate(ctx, tx, request)
}

// Siblings reads the rows of one operation still inside retention, oldest
// attempt first.
func Siblings(ctx context.Context, tx *database.Tx, operation model.ID[Operation], retained temporal.DateTime) ([]Summary, error) {
	c := Captures
	q := c.Query().Where(orm.ColumnEq(c.OperationID, operation), orm.TimeGte(c.StartedAt, retained)).OrderBy(orm.NumberAsc(c.Attempt), orm.KeyAsc(c.ID)).Limit(MaxSiblings)
	return rows.Read(Summaries(q)).All(ctx, tx)
}
