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
//foundry:model table=foundry_failed_jobs
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
