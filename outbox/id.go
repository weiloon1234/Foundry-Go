// Package outbox owns shared durable-message identities and explicit schema
// migrations. Feature-specific producers preserve their concrete payload types.
package outbox

import "github.com/weiloon1234/Foundry-Go/model"

// ID identifies a persisted message with the concrete payload owner E. It is
// distinct from a model ID and from a message with another payload owner. An ID
// does not establish successful commit, delivery, record existence or authority.
type ID[E any] = model.ID[Message[E]]

// Message is the payload-specific identity owner for durable records. It carries
// type information only; feature descriptors provide payload access. Exporting
// this owner lets generated model codecs name an outbox ID's complete Go type
// when an application persists one, without a second UUID/codec implementation.
type Message[E any] struct{ _ [0]*E }

// ParseID is an explicit transport boundary that reapplies the expected payload
// owner. The feature's typed reload also validates destination, name and version.
func ParseID[E any](text string) (ID[E], error) { return model.ParseID[Message[E]](text) }

// IDFromBytes restores a payload owner at an explicit persistence boundary.
// UUID parsing, formatting and serialization are owned by the shared model ID.
func IDFromBytes[E any](data [16]byte) ID[E] { return model.IDFromBytes[Message[E]](data) }
