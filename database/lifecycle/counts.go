package lifecycle

import "reflect"

// RegistrationCounts is an owned value containing registration counts only.
// It cannot dispatch callbacks or identify their adapters. Example: counts.Write == 2.
type RegistrationCounts struct {
	// Write counts ordinary write registrations. Example: counts.Write == 2.
	Write int
	// Retrieval counts complete-model read registrations. Example: counts.Retrieval == 1.
	Retrieval int
	// DeletionOnly counts registrations restricted to deletion operations.
	// Example: counts.Write + counts.DeletionOnly is the deletion dispatch count.
	DeletionOnly int
}

// CountObservers inspects exactly M without invoking or exposing factories.
// The zero set has zero counts. It describes the supplied view: ForDeletion merges
// deletion-only registrations into Write and leaves DeletionOnly zero.
// Example: counts := lifecycle.CountObservers[Order](rows.Observers()).
func CountObservers[M any](set Observers) RegistrationCounts {
	model := reflect.TypeFor[M]()
	return RegistrationCounts{
		Write:        len(set.groups[observerGroupKey{model: model, kind: writeObserver}].factories),
		Retrieval:    len(set.groups[observerGroupKey{model: model, kind: retrievalObserver}].factories),
		DeletionOnly: len(set.groups[observerGroupKey{model: model, kind: deletionObserver}].factories),
	}
}
