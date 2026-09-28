package eventqueries

import "github.com/weiloon1234/Foundry-Go/outbox"

// EventLink verifies that a payload-owned message ID remains usable as an
// ordinary generated model field, including codecs, predicates and draft setters.
// It is a consumer fixture record, not an outbox delivery or acknowledgement API.
//
//foundry:model table=event_links primary=ID
type EventLink struct {
	ID        int64
	MessageID outbox.ID[RecordCreated]
}
