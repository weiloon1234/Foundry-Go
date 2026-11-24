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
		rows.Map(s.ID, expr.Value(c.ID)),
		rows.Map(s.ReceivedAt, expr.Value(c.ReceivedAt)),
		rows.Map(s.Method, expr.Value(c.Method)),
		rows.Map(s.Path, expr.Value(c.Path)),
		rows.Map(s.Route, expr.Value(c.Route)),
		rows.Map(s.ClientIP, expr.Value(c.ClientIP)),
		rows.Map(s.ResponseStatus, expr.Value(c.ResponseStatus)),
		rows.Map(s.Outcome, expr.Value(c.Outcome)),
		rows.Map(s.ResponseState, expr.Value(c.ResponseState)),
		rows.Map(s.DurationMS, expr.Value(c.DurationMS)),
		rows.Map(s.BodyMode, expr.Value(c.BodyMode)),
		rows.Map(s.BodyState, expr.Value(c.BodyState)),
		rows.Map(s.BodyOmission, expr.Value(c.BodyOmission)),
		rows.Map(s.DeclaredBytes, expr.Value(c.DeclaredBytes)),
		rows.Map(s.MeasuredBytes, expr.Value(c.MeasuredBytes)),
		rows.Map(s.ObservedBytes, expr.Value(c.ObservedBytes)),
		rows.Map(s.DroppedBefore, expr.Value(c.DroppedBefore)),
	)
}
