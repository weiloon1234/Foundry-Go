package eventqueries

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

// doc-example:begin event-outbox
var Publications = foundation.NewKey[*events.Outbox]("consumer.publications")

// ConfigurePublications declares the producer; TOML maps its destination to a
// durable job connection.
func ConfigurePublications(builder *application.Builder) *application.Builder {
	return builder.Events(application.Topic(Created), application.EventOutbox(Publications, "consumer.publications"))
}

// doc-example:end event-outbox

// doc-example:begin queued-listener
func QueueListener(topic events.Topic[RecordCreated], name events.ListenerID, handle events.Handler[RecordCreated]) application.EventDeclaration {
	return application.ListenQueued(topic, name, func(application.Services) (events.Handler[RecordCreated], error) {
		return handle, nil
	})
}

// doc-example:end queued-listener
