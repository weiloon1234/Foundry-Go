package outboundcapturestore

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

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
		rows.Map(s.ID, expr.Value(c.ID)),
		rows.Map(s.OperationID, expr.Value(c.OperationID)),
		rows.Map(s.Attempt, expr.Value(c.Attempt)),
		rows.Map(s.Retried, expr.Value(c.Retried)),
		rows.Map(s.StartedAt, expr.Value(c.StartedAt)),
		rows.Map(s.Client, expr.Value(c.Client)),
		rows.Map(s.Method, expr.Value(c.Method)),
		rows.Map(s.Host, expr.Value(c.Host)),
		rows.Map(s.Path, expr.Value(c.Path)),
		rows.Map(s.Status, expr.Value(c.Status)),
		rows.Map(s.Result, expr.Value(c.Result)),
		rows.Map(s.HeadersMS, expr.Value(c.HeadersMS)),
		rows.Map(s.DurationMS, expr.Value(c.DurationMS)),
		rows.Map(s.RequestID, expr.Value(c.RequestID)),
		rows.Map(s.BodyMode, expr.Value(c.BodyMode)),
		rows.Map(s.RequestSentBytes, expr.Value(c.RequestSentBytes)),
		rows.Map(s.ResponseReadBytes, expr.Value(c.ResponseReadBytes)),
		rows.Map(s.DroppedBefore, expr.Value(c.DroppedBefore)),
	)
}
