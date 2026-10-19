package eventqueries

import (
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var Publications = foundation.NewKey[*events.Outbox]("consumer.publications")

// ConfigurePublications replaces the custom producer module in a configured
// application. TOML maps consumer.publications to its durable job connection.
func ConfigurePublications(builder *application.Builder) *application.Builder {
	return builder.Events(application.Topic(Created), application.EventOutbox(Publications, "consumer.publications"))
}
