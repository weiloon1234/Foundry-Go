package database

import (
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryObservers returns the frozen observer snapshot of a concrete framework
// owner. Unknown wrappers cannot prove observer absence. The capability is
// validated here and is never passed to an application-implementable interface.
// Example (framework only): FoundryObservers(db, sqlowner.Observers()).
func FoundryObservers(writer any, seal sqlowner.ObserverSeal) (lifecycle.Observers, bool) {
	if !seal.Valid() {
		return lifecycle.Observers{}, false
	}
	switch owner := writer.(type) {
	case *DB:
		return owner.FoundryObservers(seal)
	case PrimaryExecutor:
		return owner.FoundryObservers(seal)
	case *PrimaryExecutor:
		if owner != nil {
			return owner.FoundryObservers(seal)
		}
	case *Session:
		return owner.FoundryObservers(seal)
	case *Tx:
		return owner.FoundryObservers(seal)
	}
	return lifecycle.Observers{}, false
}
