package webhookcapturestore

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Scope runs bounded transactions against the table in one validated schema
// on the borrowed primary pool. Reads use the primary too: recent captures
// may not have reached a replica yet.
type Scope struct {
	db     *database.DB
	schema string
}

// NewScope validates the borrowed database and schema without I/O.
func NewScope(db *database.DB, schema string) (Scope, error) {
	if db == nil || !sqlname.Valid(schema) {
		return Scope{}, safeerror.NewFault(fault.Invalid, "webhook capture store requires a database and valid schema")
	}
	return Scope{db: db, schema: schema}, nil
}

// Transaction scopes fn to the capture schema inside one owned transaction.
func (s Scope) Transaction(ctx context.Context, readOnly bool, fn func(*database.Tx) error) error {
	options := database.TxOptions{Isolation: database.ReadCommitted, ReadOnly: readOnly}
	return s.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, options)
}

// Exists reports whether the migrated table is visible in the schema.
func (s Scope) Exists(ctx context.Context) (bool, error) {
	var present bool
	err := database.ScanOne(ctx, s.db.Primary(), `SELECT pg_catalog.to_regclass(pg_catalog.quote_ident($1) || '.foundry_webhook_captures') IS NOT NULL`, []any{s.schema}, &present)
	return present, err
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

// Lock takes the transaction-scoped retention lock for this schema without
// waiting; replicas that lose the race skip the batch.
func Lock(ctx context.Context, tx *database.Tx, schema string) (bool, error) {
	var acquired bool
	err := database.ScanOne(ctx, tx, `SELECT pg_catalog.pg_try_advisory_xact_lock(pg_catalog.hashtext('foundry.webhook_capture'), pg_catalog.hashtext($1))`, []any{schema}, &acquired)
	return acquired, err
}

// DeleteBefore removes up to limit rows received before cutoff, oldest first,
// in one statement that never loads payloads.
func DeleteBefore(ctx context.Context, tx *database.Tx, cutoff temporal.DateTime, limit int) (int64, error) {
	c := Captures
	oldest := rows.SelectScalar(rows.From(c.Query().Where(orm.TimeLt(c.ReceivedAt, cutoff)).OrderBy(orm.TimeAsc(c.ReceivedAt), orm.KeyAsc(c.ID)).Limit(limit)), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(c.ID)))))
	return orm.DeleteAll(ctx, tx, expr.Where(c.Query(), rows.InQuery(expr.Value(orm.KeyExpr(c.ID)), oldest)))
}

// Count counts every row.
func Count(ctx context.Context, tx *database.Tx) (int64, error) {
	return orm.Count(ctx, tx, Captures.Query())
}

// CountBefore counts rows received before cutoff.
func CountBefore(ctx context.Context, tx *database.Tx, cutoff temporal.DateTime) (int64, error) {
	return orm.Count(ctx, tx, Captures.Query().Where(orm.TimeLt(Captures.ReceivedAt, cutoff)))
}

// Overflow returns the receive time of the newest row beyond keep, when the
// table holds more than keep rows. Rows received at or before it exceed the
// cap; rows sharing that microsecond with a kept row are removed too.
func Overflow(ctx context.Context, tx *database.Tx, keep int) (value.Optional[temporal.DateTime], error) {
	c := Captures
	return rows.Read(rows.SelectValue(c.Query().OrderBy(orm.TimeDesc(c.ReceivedAt), orm.KeyDesc(c.ID)).Offset(keep).Limit(1), orm.TimeExpr(c.ReceivedAt))).First(ctx, tx)
}
