// Package jobarchive owns the durable failed-job archive persistence boundary.
// The jobs/archive package exposes its typed operator API.
package jobarchive

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Failure is one archived terminal job failure: the complete envelope (payload
// included, for explicit re-dispatch) as its original transport bytes, its
// queue and bounded classification.
// It is infrastructure storage, never a public response DTO.
type Failure struct {
	ID         model.ID[Failure]
	Execution  jobs.ExecutionID
	Queue      string
	Name       string
	Version    uint32
	Envelope   string
	Reason     string
	Attempts   uint32
	Exceptions uint32
	Retries    uint32
	FailedAt   temporal.DateTime
	RetriedAt  value.Nullable[temporal.DateTime]
}

// Format keeps the archived payload out of routine diagnostics.
func (Failure) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("archived job failure")) }

// FailureTable binds the persisted Failure columns. Example: Failures.Query().Limit(10).
type FailureTable struct {
	// Table provides Failure queries and writes. Example: Failures.Query().Limit(10).
	orm.Table[Failure, model.ID[Failure]]
	// ID binds the persisted Failure.ID column.
	// Example: orm.KeyEq(Failures.ID, id).
	ID orm.Key[Failure, model.ID[Failure]]
	// Execution binds the persisted Failure.Execution column.
	// Example: orm.ColumnSet(Failures.Execution, execution).
	Execution orm.Column[Failure, jobs.ExecutionID]
	// Queue binds the persisted Failure.Queue column.
	// Example: orm.TextSet(Failures.Queue, queue).
	Queue orm.Text[Failure, string]
	// Name binds the persisted Failure.Name column.
	// Example: orm.TextSet(Failures.Name, name).
	Name orm.Text[Failure, string]
	// Version binds the persisted Failure.Version column.
	// Example: orm.NumberSet(Failures.Version, version).
	Version orm.Number[Failure, uint32]
	// Envelope binds the persisted Failure.Envelope column.
	// Example: orm.TextSet(Failures.Envelope, envelope).
	Envelope orm.Text[Failure, string]
	// Reason binds the persisted Failure.Reason column.
	// Example: orm.TextSet(Failures.Reason, reason).
	Reason orm.Text[Failure, string]
	// Attempts binds the persisted Failure.Attempts column.
	// Example: orm.NumberSet(Failures.Attempts, attempts).
	Attempts orm.Number[Failure, uint32]
	// Exceptions binds the persisted Failure.Exceptions column.
	// Example: orm.NumberSet(Failures.Exceptions, exceptions).
	Exceptions orm.Number[Failure, uint32]
	// Retries binds the persisted Failure.Retries column.
	// Example: orm.NumberSet(Failures.Retries, retries).
	Retries orm.Number[Failure, uint32]
	// FailedAt binds the persisted Failure.FailedAt column.
	// Example: orm.TimeSet(Failures.FailedAt, failedAt).
	FailedAt orm.Time[Failure, temporal.DateTime]
	// RetriedAt binds the persisted Failure.RetriedAt column.
	// Example: orm.NullableTimeSetNull(Failures.RetriedAt).
	RetriedAt orm.NullableTime[Failure, temporal.DateTime]
}

// Failures owns the immutable Failure declaration. Example: orm.First(ctx, tx, Failures.Query()).
var Failures = orm.Define("foundry_failed_jobs", func(b orm.Builder[Failure], m *Failure) FailureTable {
	table := orm.TableOf(b, &m.ID)
	return FailureTable{Table: table,
		ID:         table.Key(),
		Execution:  orm.ColumnOf(b, &m.Execution),
		Queue:      orm.TextOf(b, &m.Queue),
		Name:       orm.TextOf(b, &m.Name),
		Version:    orm.NumberOf(b, &m.Version),
		Envelope:   orm.TextOf(b, &m.Envelope),
		Reason:     orm.TextOf(b, &m.Reason),
		Attempts:   orm.NumberOf(b, &m.Attempts),
		Exceptions: orm.NumberOf(b, &m.Exceptions),
		Retries:    orm.NumberOf(b, &m.Retries),
		FailedAt:   orm.TimeOf(b, &m.FailedAt),
		RetriedAt:  orm.NullableTimeOf(b, &m.RetriedAt),
	}
})
