package store

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// ControlTable binds Control columns without managed timestamps.
type ControlTable struct {
	orm.Table[Control, string]
}

// Controls owns the foundry_telemetry_controls declaration.
var Controls = orm.Define("foundry_telemetry_controls", func(b orm.Builder[Control], m *Control) ControlTable {
	return ControlTable{Table: orm.TableOf(b, &m.Key)}
}, orm.NoTimestamps())

// RunTable binds Run columns without managed timestamps.
type RunTable[R any, I, L ~string] struct {
	orm.Table[Run[R, I, L], model.ID[Run[R, I, L]]]
	Instance    orm.Text[Run[R, I, L], I]
	Release     orm.Text[Run[R, I, L], L]
	StartedAt   orm.Time[Run[R, I, L], temporal.DateTime]
	HeartbeatAt orm.Time[Run[R, I, L], temporal.DateTime]
	RetainUntil orm.Time[Run[R, I, L], temporal.DateTime]
	Runtime     orm.JSON[Run[R, I, L], Envelope[R]]
}

// Runs owns the foundry_telemetry_runs declaration.
func Runs[R any, I, L ~string]() RunTable[R, I, L] {
	return orm.Define("foundry_telemetry_runs", func(b orm.Builder[Run[R, I, L]], m *Run[R, I, L]) RunTable[R, I, L] {
		return RunTable[R, I, L]{Table: orm.TableOf(b, &m.ID),
			Instance:    orm.TextOf(b, &m.Instance),
			Release:     orm.TextOf(b, &m.Release),
			StartedAt:   orm.TimeOf(b, &m.StartedAt),
			HeartbeatAt: orm.TimeOf(b, &m.HeartbeatAt),
			RetainUntil: orm.TimeOf(b, &m.RetainUntil),
			Runtime:     orm.JSONOf(b, &m.Runtime),
		}
	}, orm.NoTimestamps())
}

// FlushTable binds Flush columns without managed timestamps.
type FlushTable[O, P any] struct {
	orm.Table[Flush[O, P], model.ID[Flush[O, P]]]
	RunID       orm.Column[Flush[O, P], model.ID[O]]
	CreatedAt   orm.Time[Flush[O, P], temporal.DateTime]
	StoredAt    orm.Time[Flush[O, P], temporal.DateTime]
	RetainUntil orm.Time[Flush[O, P], temporal.DateTime]
	Result      orm.JSON[Flush[O, P], Envelope[P]]
}

// Flushes owns the foundry_telemetry_flushes declaration.
func Flushes[O, P any]() FlushTable[O, P] {
	return orm.Define("foundry_telemetry_flushes", func(b orm.Builder[Flush[O, P]], m *Flush[O, P]) FlushTable[O, P] {
		return FlushTable[O, P]{Table: orm.TableOf(b, &m.ID),
			RunID:       orm.ColumnOf(b, &m.RunID),
			CreatedAt:   orm.TimeOf(b, &m.CreatedAt),
			StoredAt:    orm.TimeOf(b, &m.StoredAt),
			RetainUntil: orm.TimeOf(b, &m.RetainUntil),
			Result:      orm.JSONOf(b, &m.Result),
		}
	}, orm.NoTimestamps())
}

// MetricTable binds Metric columns without managed timestamps.
type MetricTable[O any] struct {
	orm.Table[Metric[O], MetricKey]
	RunID           orm.Column[Metric[O], model.ID[O]]
	BucketAt        orm.Time[Metric[O], temporal.DateTime]
	IntervalSeconds orm.Number[Metric[O], int]
	Route           orm.Text[Metric[O], http.RouteID]
	RoutePath       orm.Text[Metric[O], string]
	Method          orm.Text[Metric[O], http.Method]
	Status          orm.Number[Metric[O], int]
	Outcome         orm.Text[Metric[O], observability.Outcome]
	Histogram       orm.JSON[Metric[O], Envelope[Histogram]]
}

// Metrics owns the foundry_telemetry_metric_buckets declaration.
func Metrics[O any]() MetricTable[O] {
	return orm.Define("foundry_telemetry_metric_buckets", func(b orm.Builder[Metric[O]], m *Metric[O]) MetricTable[O] {
		return MetricTable[O]{Table: orm.TableOf(b, &m.Key),
			RunID:           orm.ColumnOf(b, &m.RunID),
			BucketAt:        orm.TimeOf(b, &m.BucketAt),
			IntervalSeconds: orm.NumberOf(b, &m.IntervalSeconds),
			Route:           orm.TextOf(b, &m.Route),
			RoutePath:       orm.TextOf(b, &m.RoutePath),
			Method:          orm.TextOf(b, &m.Method),
			Status:          orm.NumberOf(b, &m.Status),
			Outcome:         orm.TextOf(b, &m.Outcome),
			Histogram:       orm.JSONOf(b, &m.Histogram),
		}
	}, orm.NoTimestamps())
}

// IssueTable binds Issue columns without managed timestamps.
type IssueTable[D any, S, F, L ~string] struct {
	orm.Table[Issue[D, S, F, L], F]
	Source  orm.Enum[Issue[D, S, F, L], S]
	Code    orm.Text[Issue[D, S, F, L], string]
	Route   orm.Text[Issue[D, S, F, L], http.RouteID]
	Release orm.Text[Issue[D, S, F, L], L]
	Count   orm.Number[Issue[D, S, F, L], int64]
	FirstAt orm.Time[Issue[D, S, F, L], temporal.DateTime]
	LastAt  orm.Time[Issue[D, S, F, L], temporal.DateTime]
	Detail  orm.JSON[Issue[D, S, F, L], Envelope[D]]
}

// Issues owns the foundry_telemetry_issues declaration.
func Issues[D any, S TextEnum[S], F, L ~string]() IssueTable[D, S, F, L] {
	return orm.Define("foundry_telemetry_issues", func(b orm.Builder[Issue[D, S, F, L]], m *Issue[D, S, F, L]) IssueTable[D, S, F, L] {
		return IssueTable[D, S, F, L]{Table: orm.TableOf(b, &m.Fingerprint),
			Source:  orm.EnumOf(b, &m.Source),
			Code:    orm.TextOf(b, &m.Code),
			Route:   orm.TextOf(b, &m.Route),
			Release: orm.TextOf(b, &m.Release),
			Count:   orm.NumberOf(b, &m.Count),
			FirstAt: orm.TimeOf(b, &m.FirstAt),
			LastAt:  orm.TimeOf(b, &m.LastAt),
			Detail:  orm.JSONOf(b, &m.Detail),
		}
	}, orm.NoTimestamps())
}

// EventTable binds Event columns without managed timestamps.
type EventTable[D any, S, F ~string] struct {
	orm.Table[Event[D, S, F], model.ID[Event[D, S, F]]]
	Fingerprint orm.Text[Event[D, S, F], F]
	Source      orm.Enum[Event[D, S, F], S]
	CreatedAt   orm.Time[Event[D, S, F], temporal.DateTime]
	Detail      orm.JSON[Event[D, S, F], Envelope[D]]
}

// Events owns the foundry_telemetry_events declaration.
func Events[D any, S TextEnum[S], F ~string]() EventTable[D, S, F] {
	return orm.Define("foundry_telemetry_events", func(b orm.Builder[Event[D, S, F]], m *Event[D, S, F]) EventTable[D, S, F] {
		return EventTable[D, S, F]{Table: orm.TableOf(b, &m.ID),
			Fingerprint: orm.TextOf(b, &m.Fingerprint),
			Source:      orm.EnumOf(b, &m.Source),
			CreatedAt:   orm.TimeOf(b, &m.CreatedAt),
			Detail:      orm.JSONOf(b, &m.Detail),
		}
	}, orm.NoTimestamps())
}

// AlertTable binds Alert columns without managed timestamps.
type AlertTable[O any, Q, S, K ~string, E ~uint64] struct {
	orm.Table[Alert[O, Q, S, K, E], K]
	RunID       orm.Column[Alert[O, Q, S, K, E], model.ID[O]]
	Rule        orm.Enum[Alert[O, Q, S, K, E], Q]
	State       orm.Enum[Alert[O, Q, S, K, E], S]
	Episode     orm.Number[Alert[O, Q, S, K, E], E]
	FirstAt     orm.Time[Alert[O, Q, S, K, E], temporal.DateTime]
	LastAt      orm.Time[Alert[O, Q, S, K, E], temporal.DateTime]
	RecoveredAt orm.NullableTime[Alert[O, Q, S, K, E], temporal.DateTime]
}

// Alerts owns the foundry_telemetry_alerts declaration.
func Alerts[O any, Q TextEnum[Q], S TextEnum[S], K ~string, E ~uint64]() AlertTable[O, Q, S, K, E] {
	return orm.Define("foundry_telemetry_alerts", func(b orm.Builder[Alert[O, Q, S, K, E]], m *Alert[O, Q, S, K, E]) AlertTable[O, Q, S, K, E] {
		return AlertTable[O, Q, S, K, E]{Table: orm.TableOf(b, &m.Key),
			RunID:       orm.ColumnOf(b, &m.RunID),
			Rule:        orm.EnumOf(b, &m.Rule),
			State:       orm.EnumOf(b, &m.State),
			Episode:     orm.NumberOf(b, &m.Episode),
			FirstAt:     orm.TimeOf(b, &m.FirstAt),
			LastAt:      orm.TimeOf(b, &m.LastAt),
			RecoveredAt: orm.NullableTimeOf(b, &m.RecoveredAt),
		}
	}, orm.NoTimestamps())
}
