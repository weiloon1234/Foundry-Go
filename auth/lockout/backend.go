package lockout

import "context"

// Backend owns atomic transitions and the authority clock. Begin records the
// candidate generation only when creating a new window. Finish must use the exact
// admitted Snapshot once; it checks current lock/expiry even after successful
// credential verification. Success clears only when the revision still matches.
// Reset invalidates all outstanding snapshots for that key. Live policy changes,
// corrupt state and backward clocks fail closed. No operation retries uncertain
// mutations or falls back to another authority. The caller owns lifecycle.
//
// Backend methods are trusted adapter boundaries, not public credential proof.
// An error always returns a zero result. External adapters must provide bounded
// state, atomic count+expiry updates and distinct generation values after reset.
type Backend interface {
	// LockoutBegin checks the current lock and returns a snapshot for an admitted attempt,
	// using candidate only when creating a window.
	LockoutBegin(context.Context, Key, Policy, Generation) (Admission, error)
	// LockoutFinish applies one admitted outcome after checking the current generation,
	// revision and lock. A stale success cannot clear newer failures.
	LockoutFinish(context.Context, Key, Policy, Snapshot, Outcome) (Decision, error)
	// LockoutReset removes the key's state and invalidates outstanding snapshots,
	// reporting whether state existed.
	LockoutReset(context.Context, Key, Policy) (bool, error)
}
