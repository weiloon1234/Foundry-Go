package compilefail

import (
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/testkit/events"
)

func invalid() {
	recorder, _ := events.New[string](1)
	_, _ = eventqueries.Created.Declare(recorder.Listener("recorder"))
}
