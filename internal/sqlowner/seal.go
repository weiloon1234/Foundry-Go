// Package sqlowner owns validated, family-specific framework database capabilities.
package sqlowner

type sentinel struct{ live byte }

// MetadataSeal authorizes only the metadata bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type MetadataSeal struct{ metadata *sentinel }

var metadataCapability = sentinel{live: 1}

// Metadata issues a framework metadata capability.
func Metadata() MetadataSeal { return MetadataSeal{metadata: &metadataCapability} }

// Valid rejects zero values and foreign sentinels.
func (s MetadataSeal) Valid() bool { return s.metadata == &metadataCapability }

// ExpressionSeal authorizes only the expression bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type ExpressionSeal struct{ expression *sentinel }

var expressionCapability = sentinel{live: 1}

// Expressions issues a framework expression capability.
func Expressions() ExpressionSeal { return ExpressionSeal{expression: &expressionCapability} }

// Valid rejects zero values and foreign sentinels.
func (s ExpressionSeal) Valid() bool { return s.expression == &expressionCapability }

// QuerySeal authorizes only the query bridge family. Its zero value is invalid.
// The distinct private field also prevents conversion between capability families.
type QuerySeal struct{ query *sentinel }

var queryCapability = sentinel{live: 1}

// Queries issues a framework query capability.
func Queries() QuerySeal { return QuerySeal{query: &queryCapability} }

// Valid rejects zero values and foreign sentinels.
func (s QuerySeal) Valid() bool { return s.query == &queryCapability }

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
