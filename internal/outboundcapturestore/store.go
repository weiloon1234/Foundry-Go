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
			orm.Set(c.ID, row.ID),
			orm.Set(c.OperationID, row.OperationID),
			orm.Set(c.Attempt, row.Attempt),
			orm.Set(c.Retried, row.Retried),
			orm.Set(c.Client, row.Client),
			orm.Set(c.StartedAt, row.StartedAt),
			nullableNumber(c.HeadersMS, row.HeadersMS),
			orm.Set(c.DurationMS, row.DurationMS),
			orm.Set(c.Result, row.Result),
			nullableText(c.RequestID, row.RequestID),
			nullableText(c.TraceID, row.TraceID),
			nullableText(c.Route, row.Route),
			orm.Set(c.Method, row.Method),
			orm.Set(c.Scheme, row.Scheme),
			orm.Set(c.Host, row.Host),
			orm.Set(c.Path, row.Path),
			orm.Set(c.QueryFields, row.Query),
			orm.Set(c.RequestHeaders, row.RequestHeaders),
			orm.Set(c.BodyMode, row.BodyMode),
			orm.Set(c.RequestBodyState, row.RequestBodyState),
			nullableText(c.RequestBodyOmission, row.RequestBodyOmission),
			orm.Set(c.RequestBody, row.RequestBody),
			nullableNumber(c.RequestDeclaredBytes, row.RequestDeclaredBytes),
			orm.Set(c.RequestSentBytes, row.RequestSentBytes),
			orm.Set(c.RequestBodyComplete, row.RequestBodyComplete),
			nullableNumber(c.Status, row.Status),
			nullableText(c.Protocol, row.Protocol),
			nullableText(c.RemoteIP, row.RemoteIP),
			nullableBool(c.ConnectionReused, row.ConnectionReused),
			orm.Set(c.ResponseHeaders, row.ResponseHeaders),
			orm.Set(c.ResponseBodyState, row.ResponseBodyState),
			nullableText(c.ResponseBodyOmission, row.ResponseBodyOmission),
			orm.Set(c.ResponseBody, row.ResponseBody),
			nullableNumber(c.ResponseDeclaredBytes, row.ResponseDeclaredBytes),
			orm.Set(c.ResponseReadBytes, row.ResponseReadBytes),
			orm.Set(c.ResponseBodyComplete, row.ResponseBodyComplete),
			orm.Set(c.Omissions, row.Omissions),
			orm.Set(c.DroppedBefore, row.DroppedBefore),
		})
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

func nullableBool(column orm.NullableColumn[Capture, bool], v interface{ Get() (bool, bool) }) orm.Assignment[Capture] {
	if flag, present := v.Get(); present {
		return orm.Set(column, flag)
	}
	return orm.SetNull(column)
}

// Find loads one capture still inside retention, or database.NotFound.
func Find(ctx context.Context, tx *database.Tx, id model.ID[Capture], retained temporal.DateTime) (Capture, error) {
	return orm.Require(ctx, tx, Captures.Query().Where(orm.Eq(Captures.ID, id), orm.Gte(Captures.StartedAt, retained)))
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
	q := c.Query().Where(orm.Eq(c.OperationID, operation), orm.Gte(c.StartedAt, retained)).OrderBy(orm.Asc(c.Attempt), orm.Asc(c.ID)).Limit(MaxSiblings)
	return rows.Read(Summaries(q)).All(ctx, tx)
}
