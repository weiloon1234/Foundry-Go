package telemetry

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/telemetry/internal/store"
)

type runRow = store.Run[Runtime, Instance, Release]
type flushRow = store.Flush[runRow, receiptState]
type metricRow = store.Metric[runRow]
type issueRow = store.Issue[Detail, Source, Fingerprint, Release]
type eventRow = store.Event[Detail, Source, Fingerprint]
type alertRow = store.Alert[runRow, Rule, AlertState, AlertKey, Episode]

// Declarations are immutable, shared metadata. All application/runtime state is
// owned by a storage instance. These aliases never escape the telemetry package.
// Enum construction stays in function bodies: generation checks declaration
// signatures before generated enum methods exist, while constructors require
// those descriptors when the package is compiled normally.
var (
	runs    = store.Runs[Runtime, Instance, Release]()
	flushes = store.Flushes[runRow, receiptState]()
	metrics = store.Metrics[runRow]()
	issues  = func() store.IssueTable[Detail, Source, Fingerprint, Release] {
		return store.Issues[Detail, Source, Fingerprint, Release]()
	}()
	events = func() store.EventTable[Detail, Source, Fingerprint] {
		return store.Events[Detail, Source, Fingerprint]()
	}()
	alerts = func() store.AlertTable[runRow, Rule, AlertState, AlertKey, Episode] {
		return store.Alerts[runRow, Rule, AlertState, AlertKey, Episode]()
	}()
)

func storageTables() []orm.CheckedTable {
	return []orm.CheckedTable{store.Controls, runs, flushes, metrics, issues, events, alerts}
}
