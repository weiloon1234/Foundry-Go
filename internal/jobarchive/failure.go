// Package jobarchive owns the durable failed-job archive persistence boundary.
// The jobs/archive package exposes its typed operator API.
package jobarchive

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Failure is one archived terminal job failure: the complete envelope (payload
// included, for explicit re-dispatch) as its original transport bytes, its
// queue and bounded classification.
// It is infrastructure storage, never a public response DTO.
//
//foundry:model table=foundry_failed_jobs var=Failures key=ID identity=false
type Failure struct {
	// Table provides Failure queries and writes. Example: Failures.Query().Limit(10).
	//
	// ID binds the persisted Failure.ID column.
	// Example: orm.Eq(Failures.ID, id).
	ID model.ID[Failure] `foundry:"member=ID"`
	// Execution binds the persisted Failure.Execution column.
	// Example: orm.Set(Failures.Execution, execution).
	Execution jobs.ExecutionID `foundry:"role=column"`
	// Queue binds the persisted Failure.Queue column.
	// Example: orm.Set(Failures.Queue, queue).
	Queue string
	// Name binds the persisted Failure.Name column.
	// Example: orm.Set(Failures.Name, name).
	Name string
	// Version binds the persisted Failure.Version column.
	// Example: orm.Set(Failures.Version, version).
	Version uint32
	// Envelope binds the persisted Failure.Envelope column.
	// Example: orm.Set(Failures.Envelope, envelope).
	Envelope string
	// Reason binds the persisted Failure.Reason column.
	// Example: orm.Set(Failures.Reason, reason).
	Reason string
	// Attempts binds the persisted Failure.Attempts column.
	// Example: orm.Set(Failures.Attempts, attempts).
	Attempts uint32
	// Exceptions binds the persisted Failure.Exceptions column.
	// Example: orm.Set(Failures.Exceptions, exceptions).
	Exceptions uint32
	// Retries binds the persisted Failure.Retries column.
	// Example: orm.Set(Failures.Retries, retries).
	Retries uint32
	// FailedAt binds the persisted Failure.FailedAt column.
	// Example: orm.Set(Failures.FailedAt, failedAt).
	FailedAt temporal.DateTime
	// RetriedAt binds the persisted Failure.RetriedAt column.
	// Example: orm.SetNull(Failures.RetriedAt).
	RetriedAt value.Nullable[temporal.DateTime]
}

// Format keeps the archived payload out of routine diagnostics.
func (Failure) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("archived job failure")) }
