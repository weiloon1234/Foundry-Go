package telemetry

import (
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// operationOwner includes reads, pruning, test flushes and caller-transaction
// acknowledgements in the engine's dependency barrier. The zero value admits work.
type operationOwner struct {
	mu     sync.Mutex
	active int
	sealed bool
	done   chan struct{}
}

func (o *operationOwner) enter() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sealed {
		return safeerror.NewFault(fault.Closed, "telemetry is closing")
	}
	o.active++
	return nil
}

func (o *operationOwner) leave() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active--
	if o.sealed && o.active == 0 {
		close(o.done)
	}
}

func (o *operationOwner) seal() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.sealed {
		o.sealed = true
		o.done = make(chan struct{})
		if o.active == 0 {
			close(o.done)
		}
	}
	return o.done
}
