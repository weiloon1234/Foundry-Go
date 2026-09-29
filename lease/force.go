package lease

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// ForceBackend is an optional administrative capability that removes a lease
// address regardless of its owner, including metadata that ordinary operations
// reject as corrupt (for example a Redis lease key without expiry). It is never
// used implicitly; ordinary acquisition, renewal and release stay owner-checked.
type ForceBackend interface {
	LeaseForceRelease(context.Context, Key) (bool, error)
}

// ForceRelease administratively removes key's lease whatever its owner. Use it
// to repair corrupt or non-expiring lease metadata, or to break ownership after
// confirming the owning process is gone. It reports whether a lease existed.
// A current holder is not notified: it loses ownership at its next renewal or
// protected write, so resources that need stale-process exclusion still need
// their own fencing. The backend must implement ForceBackend.
func (l Leases[K]) ForceRelease(ctx context.Context, key K) (bool, error) {
	if l.definition == nil {
		return false, fault.New(fault.Invalid, "lease handle is not initialized")
	}
	backend, ok := l.manager.backend.(ForceBackend)
	if !ok {
		return false, fault.New(fault.Invalid, "lease backend does not support forced release")
	}
	if err := l.manager.begin(ctx); err != nil {
		return false, err
	}
	defer l.manager.end()
	command, cancel := context.WithTimeout(ctx, l.manager.config.OperationTimeout)
	defer cancel()
	address, err := l.address(command, key)
	if err != nil {
		return false, err
	}
	return l.manager.command(command, func(ctx context.Context) (bool, error) {
		return backend.LeaseForceRelease(ctx, address)
	})
}
