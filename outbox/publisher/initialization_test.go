package publisher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

func TestPublisherRejectsUninitializedUse(t *testing.T) {
	for _, p := range []*publisher.Publisher{nil, {}} {
		for _, ctx := range []context.Context{nil, t.Context()} {
			if result, err := p.PublishOne(ctx); !errors.Is(err, fault.Invalid) || result.Found || result.Committed {
				t.Fatal("uninitialized publication accepted")
			}
			if results, err := p.PublishBatch(ctx); !errors.Is(err, fault.Invalid) || len(results) != 0 {
				t.Fatal("uninitialized batch accepted")
			}
			if err := p.Run(ctx); !errors.Is(err, fault.Invalid) {
				t.Fatal("uninitialized run accepted")
			}
		}
	}
}
