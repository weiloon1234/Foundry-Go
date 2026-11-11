package outboundcapturestore

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CaptureTable binds the persisted Capture columns. StartedAt is the
// attempt's start, not a managed creation timestamp.
type CaptureTable struct {
	orm.Table[Capture, model.ID[Capture]]
	ID          orm.Key[Capture, model.ID[Capture]]
	OperationID orm.Column[Capture, model.ID[Operation]]
	Attempt     orm.Number[Capture, int16]
	Retried     orm.Column[Capture, bool]
	Client      orm.Text[Capture, string]
	StartedAt   orm.Time[Capture, temporal.DateTime]
	HeadersMS   orm.NullableNumber[Capture, int64]
	DurationMS  orm.Number[Capture, int64]
	Result      orm.Text[Capture, string]
	RequestID   orm.NullableText[Capture, string]
	TraceID     orm.NullableText[Capture, string]
	Route       orm.NullableText[Capture, string]
	Method      orm.Text[Capture, string]
	Scheme      orm.Text[Capture, string]
	Host        orm.Text[Capture, string]
	Path        orm.Text[Capture, string]
	// QueryFields binds Capture.Query; the name avoids Table.Query.
	QueryFields           orm.JSON[Capture, []httpcapture.Field]
	RequestHeaders        orm.JSON[Capture, []httpcapture.Field]
	BodyMode              orm.Text[Capture, string]
	RequestBodyState      orm.Text[Capture, string]
	RequestBodyOmission   orm.NullableText[Capture, string]
	RequestBody           orm.Column[Capture, []byte]
	RequestDeclaredBytes  orm.NullableNumber[Capture, int64]
	RequestSentBytes      orm.Number[Capture, int64]
	RequestBodyComplete   orm.Column[Capture, bool]
	Status                orm.NullableNumber[Capture, int16]
	Protocol              orm.NullableText[Capture, string]
	RemoteIP              orm.NullableText[Capture, string]
	ConnectionReused      orm.NullableColumn[Capture, bool]
	ResponseHeaders       orm.JSON[Capture, []httpcapture.Field]
	ResponseBodyState     orm.Text[Capture, string]
	ResponseBodyOmission  orm.NullableText[Capture, string]
	ResponseBody          orm.Column[Capture, []byte]
	ResponseDeclaredBytes orm.NullableNumber[Capture, int64]
	ResponseReadBytes     orm.Number[Capture, int64]
	ResponseBodyComplete  orm.Column[Capture, bool]
	Omissions             orm.JSON[Capture, []string]
	DroppedBefore         orm.Number[Capture, int64]
}

// Captures owns the immutable declaration. Example: orm.Find(ctx, tx, Captures.Table, id).
var Captures = orm.Define("foundry_outbound_captures", func(b orm.Builder[Capture], m *Capture) CaptureTable {
	t := orm.TableOf(b, &m.ID)
	return CaptureTable{
		Table: t, ID: t.Key(),
		OperationID:           orm.IDOf(b, &m.OperationID),
		Attempt:               orm.NumberOf(b, &m.Attempt),
		Retried:               orm.BoolOf(b, &m.Retried),
		Client:                orm.TextOf(b, &m.Client),
		StartedAt:             orm.TimeOf(b, &m.StartedAt),
		HeadersMS:             orm.NullableNumberOf(b, &m.HeadersMS),
		DurationMS:            orm.NumberOf(b, &m.DurationMS),
		Result:                orm.TextOf(b, &m.Result),
		RequestID:             orm.NullableTextOf(b, &m.RequestID),
		TraceID:               orm.NullableTextOf(b, &m.TraceID),
		Route:                 orm.NullableTextOf(b, &m.Route),
		Method:                orm.TextOf(b, &m.Method),
		Scheme:                orm.TextOf(b, &m.Scheme),
		Host:                  orm.TextOf(b, &m.Host),
		Path:                  orm.TextOf(b, &m.Path),
		QueryFields:           orm.JSONOf(b, &m.Query),
		RequestHeaders:        orm.JSONOf(b, &m.RequestHeaders),
		BodyMode:              orm.TextOf(b, &m.BodyMode),
		RequestBodyState:      orm.TextOf(b, &m.RequestBodyState),
		RequestBodyOmission:   orm.NullableTextOf(b, &m.RequestBodyOmission),
		RequestBody:           orm.BytesOf(b, &m.RequestBody),
		RequestDeclaredBytes:  orm.NullableNumberOf(b, &m.RequestDeclaredBytes),
		RequestSentBytes:      orm.NumberOf(b, &m.RequestSentBytes),
		RequestBodyComplete:   orm.BoolOf(b, &m.RequestBodyComplete),
		Status:                orm.NullableNumberOf(b, &m.Status),
		Protocol:              orm.NullableTextOf(b, &m.Protocol),
		RemoteIP:              orm.NullableTextOf(b, &m.RemoteIP),
		ConnectionReused:      orm.NullableColumnOf(b, &m.ConnectionReused),
		ResponseHeaders:       orm.JSONOf(b, &m.ResponseHeaders),
		ResponseBodyState:     orm.TextOf(b, &m.ResponseBodyState),
		ResponseBodyOmission:  orm.NullableTextOf(b, &m.ResponseBodyOmission),
		ResponseBody:          orm.BytesOf(b, &m.ResponseBody),
		ResponseDeclaredBytes: orm.NullableNumberOf(b, &m.ResponseDeclaredBytes),
		ResponseReadBytes:     orm.NumberOf(b, &m.ResponseReadBytes),
		ResponseBodyComplete:  orm.BoolOf(b, &m.ResponseBodyComplete),
		Omissions:             orm.JSONOf(b, &m.Omissions),
		DroppedBefore:         orm.NumberOf(b, &m.DroppedBefore),
	}
}, orm.NoTimestamps())

type summaryFields struct {
	rows.Row[Summary]
	ID                rows.Field[Summary, model.ID[Capture]]
	OperationID       rows.Field[Summary, model.ID[Operation]]
	Attempt           rows.Field[Summary, int16]
	Retried           rows.Field[Summary, bool]
	StartedAt         rows.Field[Summary, temporal.DateTime]
	Client            rows.Field[Summary, string]
	Method            rows.Field[Summary, string]
	Host              rows.Field[Summary, string]
	Path              rows.Field[Summary, string]
	Status            rows.Field[Summary, value.Nullable[int16]]
	Result            rows.Field[Summary, string]
	HeadersMS         rows.Field[Summary, value.Nullable[int64]]
	DurationMS        rows.Field[Summary, int64]
	RequestID         rows.Field[Summary, value.Nullable[string]]
	BodyMode          rows.Field[Summary, string]
	RequestSentBytes  rows.Field[Summary, int64]
	ResponseReadBytes rows.Field[Summary, int64]
	DroppedBefore     rows.Field[Summary, int64]
}

var summaryColumns = rows.Define(func(b rows.Builder[Summary], r *Summary) summaryFields {
	return summaryFields{
		Row:               b.Row(),
		ID:                rows.IDOf(b, &r.ID),
		OperationID:       rows.IDOf(b, &r.OperationID),
		Attempt:           rows.NumberOf(b, &r.Attempt),
		Retried:           rows.BoolOf(b, &r.Retried),
		StartedAt:         rows.TimeOf(b, &r.StartedAt),
		Client:            rows.TextOf(b, &r.Client),
		Method:            rows.TextOf(b, &r.Method),
		Host:              rows.TextOf(b, &r.Host),
		Path:              rows.TextOf(b, &r.Path),
		Status:            rows.NullableNumberOf(b, &r.Status),
		Result:            rows.TextOf(b, &r.Result),
		HeadersMS:         rows.NullableNumberOf(b, &r.HeadersMS),
		DurationMS:        rows.NumberOf(b, &r.DurationMS),
		RequestID:         rows.NullableTextOf(b, &r.RequestID),
		BodyMode:          rows.TextOf(b, &r.BodyMode),
		RequestSentBytes:  rows.NumberOf(b, &r.RequestSentBytes),
		ResponseReadBytes: rows.NumberOf(b, &r.ResponseReadBytes),
		DroppedBefore:     rows.NumberOf(b, &r.DroppedBefore),
	}
})

// Summaries selects only the narrow list fields. Example: Summaries(Captures.Query()).
func Summaries(q orm.Query[Capture]) rows.Query[Capture, Summary] {
	c, s := Captures, summaryColumns
	return rows.Select(rows.From(q), s,
		rows.Map(s.ID, expr.Value(orm.KeyExpr(c.ID))),
		rows.Map(s.OperationID, expr.Value(orm.ColumnExpr(c.OperationID))),
		rows.Map(s.Attempt, expr.Value(orm.NumberExpr(c.Attempt))),
		rows.Map(s.Retried, expr.Value(orm.ColumnExpr(c.Retried))),
		rows.Map(s.StartedAt, expr.Value(orm.TimeExpr(c.StartedAt))),
		rows.Map(s.Client, expr.Value(orm.TextExpr(c.Client))),
		rows.Map(s.Method, expr.Value(orm.TextExpr(c.Method))),
		rows.Map(s.Host, expr.Value(orm.TextExpr(c.Host))),
		rows.Map(s.Path, expr.Value(orm.TextExpr(c.Path))),
		rows.Map(s.Status, expr.Value(orm.NullableNumberExpr(c.Status))),
		rows.Map(s.Result, expr.Value(orm.TextExpr(c.Result))),
		rows.Map(s.HeadersMS, expr.Value(orm.NullableNumberExpr(c.HeadersMS))),
		rows.Map(s.DurationMS, expr.Value(orm.NumberExpr(c.DurationMS))),
		rows.Map(s.RequestID, expr.Value(orm.NullableTextExpr(c.RequestID))),
		rows.Map(s.BodyMode, expr.Value(orm.TextExpr(c.BodyMode))),
		rows.Map(s.RequestSentBytes, expr.Value(orm.NumberExpr(c.RequestSentBytes))),
		rows.Map(s.ResponseReadBytes, expr.Value(orm.NumberExpr(c.ResponseReadBytes))),
		rows.Map(s.DroppedBefore, expr.Value(orm.NumberExpr(c.DroppedBefore))),
	)
}
