package websocket

import (
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"testing"
)

func TestPublicPublicationClassificationContainsMethods(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		if NotPublished(cause) || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("incomplete graph established a safe retry or authentication failure")
		}
	}
}
