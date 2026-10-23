// Package sqlowner owns validated, family-specific framework database capabilities.
package sqlowner

type sentinel struct{ live byte }

// ObserverSeal authorizes only the observer bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type ObserverSeal struct{ observer *sentinel }

var observerCapability = sentinel{live: 1}

// Observers issues a framework observer capability.
func Observers() ObserverSeal { return ObserverSeal{observer: &observerCapability} }

// Valid rejects zero values and foreign sentinels.
func (s ObserverSeal) Valid() bool { return s.observer == &observerCapability }

// AutocommitSeal authorizes only the autocommit bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type AutocommitSeal struct{ autocommit *sentinel }

var autocommitCapability = sentinel{live: 1}

// Autocommit issues a framework autocommit capability.
func Autocommit() AutocommitSeal { return AutocommitSeal{autocommit: &autocommitCapability} }

// Valid rejects zero values and foreign sentinels.
func (s AutocommitSeal) Valid() bool { return s.autocommit == &autocommitCapability }

// OutcomeSeal authorizes only the outcome bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type OutcomeSeal struct{ outcome *sentinel }

var outcomeCapability = sentinel{live: 1}

// Outcomes issues a framework outcome capability.
func Outcomes() OutcomeSeal { return OutcomeSeal{outcome: &outcomeCapability} }

// Valid rejects zero values and foreign sentinels.
func (s OutcomeSeal) Valid() bool { return s.outcome == &outcomeCapability }

// CollectionSeal authorizes only the collection bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type CollectionSeal struct{ collection *sentinel }

var collectionCapability = sentinel{live: 1}

// Collections issues a framework collection capability.
func Collections() CollectionSeal { return CollectionSeal{collection: &collectionCapability} }

// Valid rejects zero values and foreign sentinels.
func (s CollectionSeal) Valid() bool { return s.collection == &collectionCapability }
