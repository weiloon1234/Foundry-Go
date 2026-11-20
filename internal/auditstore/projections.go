package auditstore

import (
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type activityFields struct {
	rows.Row[Activity]
	ID            rows.Field[Activity, model.ID[Entry]]
	Sequence      rows.Field[Activity, int64]
	Area          rows.Field[Activity, string]
	Operation     rows.Field[Activity, lifecycle.Operation]
	Action        rows.Field[Activity, string]
	Version       rows.Field[Activity, uint32]
	Subject       rows.Field[Activity, value.Nullable[value.JSON[model.Identity]]]
	Origin        rows.Field[Activity, value.JSON[attribution.Origin]]
	Correlation   rows.Field[Activity, value.Nullable[string]]
	RequestMethod rows.Field[Activity, string]
	RequestRoute  rows.Field[Activity, string]
	CreatedAt     rows.Field[Activity, temporal.DateTime]
}

var activityColumns = rows.Define(func(b rows.Builder[Activity], r *Activity) activityFields {
	return activityFields{Row: b.Row(), ID: rows.IDOf(b, &r.ID), Sequence: rows.NumberOf(b, &r.Sequence), Area: rows.TextOf(b, &r.Area), Operation: rows.NumberOf(b, &r.Operation), Action: rows.TextOf(b, &r.Action), Version: rows.NumberOf(b, &r.Version), Subject: rows.NullableFieldOf(b, &r.Subject, orm.Codec(codec.JSON[model.Identity]())), Origin: rows.FieldOf(b, &r.Origin, orm.Codec(codec.JSON[attribution.Origin]())), Correlation: rows.NullableFieldOf(b, &r.Correlation, orm.Codec(codec.String[string]())), RequestMethod: rows.TextOf(b, &r.RequestMethod), RequestRoute: rows.TextOf(b, &r.RequestRoute), CreatedAt: rows.TimeOf(b, &r.CreatedAt)}
})

// Activities selects only the declared Activity fields. Example: Activities(Entries.Query()).All(ctx,tx).
func Activities(q orm.Query[Entry]) rows.Query[Entry, Activity] {
	return rows.Select(rows.From(q), activityColumns, rows.Map(activityColumns.ID, expr.Value(Entries.ID)), rows.Map(activityColumns.Sequence, expr.Value(Entries.Sequence)), rows.Map(activityColumns.Area, expr.Value(Entries.Area)), rows.Map(activityColumns.Operation, expr.Value(Entries.Operation)), rows.Map(activityColumns.Action, expr.Value(Entries.Action)), rows.Map(activityColumns.Version, expr.Value(Entries.Version)), rows.Map(activityColumns.Subject, expr.Value(Entries.Subject)), rows.Map(activityColumns.Origin, expr.Value(Entries.Origin)), rows.Map(activityColumns.Correlation, expr.Value(Entries.Correlation)), rows.Map(activityColumns.RequestMethod, expr.Value(Entries.RequestMethod)), rows.Map(activityColumns.RequestRoute, expr.Value(Entries.RequestRoute)), rows.Map(activityColumns.CreatedAt, expr.Value(Entries.CreatedAt)))
}
