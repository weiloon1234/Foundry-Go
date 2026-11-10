package webhookcapturestore

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CaptureTable binds the persisted Capture columns. ReceivedAt is the
// kernel's event time, not a managed creation timestamp.
type CaptureTable struct {
	orm.Table[Capture, model.ID[Capture]]
	ID         orm.Key[Capture, model.ID[Capture]]
	ReceivedAt orm.Time[Capture, temporal.DateTime]
	Method     orm.Text[Capture, string]
	Path       orm.Text[Capture, string]
	Route      orm.NullableText[Capture, string]
	ClientIP   orm.NullableText[Capture, string]
	Headers    orm.JSON[Capture, []Field]
	// QueryFields binds Capture.Query; the name avoids Table.Query.
	QueryFields         orm.JSON[Capture, []Field]
	ContentType         orm.Text[Capture, string]
	ContentEncoding     orm.Text[Capture, string]
	DeclaredBytes       orm.NullableNumber[Capture, int64]
	MeasuredBytes       orm.NullableNumber[Capture, int64]
	ObservedBytes       orm.Number[Capture, int64]
	BodyMode            orm.Text[Capture, string]
	BodyState           orm.Text[Capture, string]
	BodyOmission        orm.NullableText[Capture, string]
	Body                orm.Column[Capture, []byte]
	ResponseStatus      orm.NullableNumber[Capture, int32]
	Outcome             orm.Text[Capture, observability.Outcome]
	ResponseState       orm.Text[Capture, string]
	ResponseContentType orm.Text[Capture, string]
	ResponseEncoding    orm.Text[Capture, string]
	ResponseBytes       orm.Number[Capture, int64]
	ResponseHead        orm.Column[Capture, []byte]
	DurationMS          orm.Number[Capture, int64]
	Omissions           orm.JSON[Capture, []string]
	DroppedBefore       orm.Number[Capture, int64]
}

// Captures owns the immutable declaration. Example: orm.Find(ctx, tx, Captures.Table, id).
var Captures = orm.Define("foundry_webhook_captures", func(b orm.Builder[Capture], m *Capture) CaptureTable {
	t := orm.TableOf(b, &m.ID)
	return CaptureTable{
		Table: t, ID: t.Key(),
		ReceivedAt:          orm.TimeOf(b, &m.ReceivedAt),
		Method:              orm.TextOf(b, &m.Method),
		Path:                orm.TextOf(b, &m.Path),
		Route:               orm.NullableTextOf(b, &m.Route),
		ClientIP:            orm.NullableTextOf(b, &m.ClientIP),
		Headers:             orm.JSONOf(b, &m.Headers),
		QueryFields:         orm.JSONOf(b, &m.Query),
		ContentType:         orm.TextOf(b, &m.ContentType),
		ContentEncoding:     orm.TextOf(b, &m.ContentEncoding),
		DeclaredBytes:       orm.NullableNumberOf(b, &m.DeclaredBytes),
		MeasuredBytes:       orm.NullableNumberOf(b, &m.MeasuredBytes),
		ObservedBytes:       orm.NumberOf(b, &m.ObservedBytes),
		BodyMode:            orm.TextOf(b, &m.BodyMode),
		BodyState:           orm.TextOf(b, &m.BodyState),
		BodyOmission:        orm.NullableTextOf(b, &m.BodyOmission),
		Body:                orm.BytesOf(b, &m.Body),
		ResponseStatus:      orm.NullableNumberOf(b, &m.ResponseStatus),
		Outcome:             orm.TextOf(b, &m.Outcome),
		ResponseState:       orm.TextOf(b, &m.ResponseState),
		ResponseContentType: orm.TextOf(b, &m.ResponseContentType),
		ResponseEncoding:    orm.TextOf(b, &m.ResponseEncoding),
		ResponseBytes:       orm.NumberOf(b, &m.ResponseBytes),
		ResponseHead:        orm.BytesOf(b, &m.ResponseHead),
		DurationMS:          orm.NumberOf(b, &m.DurationMS),
		Omissions:           orm.JSONOf(b, &m.Omissions),
		DroppedBefore:       orm.NumberOf(b, &m.DroppedBefore),
	}
}, orm.NoTimestamps())

type summaryFields struct {
	rows.Row[Summary]
	ID             rows.Field[Summary, model.ID[Capture]]
	ReceivedAt     rows.Field[Summary, temporal.DateTime]
	Method         rows.Field[Summary, string]
	Path           rows.Field[Summary, string]
	Route          rows.Field[Summary, value.Nullable[string]]
	ClientIP       rows.Field[Summary, value.Nullable[string]]
	ResponseStatus rows.Field[Summary, value.Nullable[int32]]
	Outcome        rows.Field[Summary, observability.Outcome]
	ResponseState  rows.Field[Summary, string]
	DurationMS     rows.Field[Summary, int64]
	BodyMode       rows.Field[Summary, string]
	BodyState      rows.Field[Summary, string]
	BodyOmission   rows.Field[Summary, value.Nullable[string]]
	DeclaredBytes  rows.Field[Summary, value.Nullable[int64]]
	MeasuredBytes  rows.Field[Summary, value.Nullable[int64]]
	ObservedBytes  rows.Field[Summary, int64]
	DroppedBefore  rows.Field[Summary, int64]
}

var summaryColumns = rows.Define(func(b rows.Builder[Summary], r *Summary) summaryFields {
	return summaryFields{
		Row:            b.Row(),
		ID:             rows.IDOf(b, &r.ID),
		ReceivedAt:     rows.TimeOf(b, &r.ReceivedAt),
		Method:         rows.TextOf(b, &r.Method),
		Path:           rows.TextOf(b, &r.Path),
		Route:          rows.NullableTextOf(b, &r.Route),
		ClientIP:       rows.NullableTextOf(b, &r.ClientIP),
		ResponseStatus: rows.NullableNumberOf(b, &r.ResponseStatus),
		Outcome:        rows.TextOf(b, &r.Outcome),
		ResponseState:  rows.TextOf(b, &r.ResponseState),
		DurationMS:     rows.NumberOf(b, &r.DurationMS),
		BodyMode:       rows.TextOf(b, &r.BodyMode),
		BodyState:      rows.TextOf(b, &r.BodyState),
		BodyOmission:   rows.NullableTextOf(b, &r.BodyOmission),
		DeclaredBytes:  rows.NullableNumberOf(b, &r.DeclaredBytes),
		MeasuredBytes:  rows.NullableNumberOf(b, &r.MeasuredBytes),
		ObservedBytes:  rows.NumberOf(b, &r.ObservedBytes),
		DroppedBefore:  rows.NumberOf(b, &r.DroppedBefore),
	}
})

// Summaries selects only the narrow list fields. Example: Summaries(Captures.Query()).All(ctx, tx).
func Summaries(q orm.Query[Capture]) rows.Query[Capture, Summary] {
	c, s := Captures, summaryColumns
	return rows.Select(rows.From(q), s,
		rows.Map(s.ID, expr.Value(orm.KeyExpr(c.ID))),
		rows.Map(s.ReceivedAt, expr.Value(orm.TimeExpr(c.ReceivedAt))),
		rows.Map(s.Method, expr.Value(orm.TextExpr(c.Method))),
		rows.Map(s.Path, expr.Value(orm.TextExpr(c.Path))),
		rows.Map(s.Route, expr.Value(orm.NullableTextExpr(c.Route))),
		rows.Map(s.ClientIP, expr.Value(orm.NullableTextExpr(c.ClientIP))),
		rows.Map(s.ResponseStatus, expr.Value(orm.NullableNumberExpr(c.ResponseStatus))),
		rows.Map(s.Outcome, expr.Value(orm.TextExpr(c.Outcome))),
		rows.Map(s.ResponseState, expr.Value(orm.TextExpr(c.ResponseState))),
		rows.Map(s.DurationMS, expr.Value(orm.NumberExpr(c.DurationMS))),
		rows.Map(s.BodyMode, expr.Value(orm.TextExpr(c.BodyMode))),
		rows.Map(s.BodyState, expr.Value(orm.TextExpr(c.BodyState))),
		rows.Map(s.BodyOmission, expr.Value(orm.NullableTextExpr(c.BodyOmission))),
		rows.Map(s.DeclaredBytes, expr.Value(orm.NullableNumberExpr(c.DeclaredBytes))),
		rows.Map(s.MeasuredBytes, expr.Value(orm.NullableNumberExpr(c.MeasuredBytes))),
		rows.Map(s.ObservedBytes, expr.Value(orm.NumberExpr(c.ObservedBytes))),
		rows.Map(s.DroppedBefore, expr.Value(orm.NumberExpr(c.DroppedBefore))),
	)
}
