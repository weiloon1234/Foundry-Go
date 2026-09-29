// Package inline is the sync job driver for tests and local development. It
// stores jobs in a bounded process-local memory authority, and its dispatcher
// runs each accepted job synchronously in the dispatching caller with the same
// admission, middleware, overlap, retry and history semantics as a worker.
//
// It is not durable: accepted work lives only in this process. A job released
// with a delay (admission, rate limits, retry backoff) runs on a later dispatch
// to the same dispatcher or an explicit Dispatcher.RunPending once due. Handler
// failures are recorded on the job, never returned from Dispatch. A dispatch
// made by a running handler executes after that handler returns, before the
// outer dispatch returns.
package inline

import (
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
)

// Backend is a memory authority that asks its dispatcher to execute inline.
type Backend struct {
	*memory.Backend
}

var _ jobs.Backend = (*Backend)(nil)
var _ jobs.InlineBackend = (*Backend)(nil)

func New(config memory.Config) (*Backend, error) {
	backend, err := memory.New(config)
	if err != nil {
		return nil, err
	}
	return &Backend{Backend: backend}, nil
}

// JobInline marks this authority as the sync driver.
func (*Backend) JobInline() bool { return true }
