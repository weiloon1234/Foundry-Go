package publisher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

type shutdownWriter struct {
	failure error
	cancel  context.CancelFunc
	calls   int
}

func (w *shutdownWriter) Transaction(ctx context.Context, _ func(*database.Tx) error, _ ...database.TxOptions) error {
	w.calls++
	if w.cancel != nil {
		w.cancel()
		<-ctx.Done()
	}
	// Drivers can return a classified failure without wrapping ctx.Err().
	return w.failure
}

func TestPublisherShutdownDoesNotRequireDriverCancellationIdentity(t *testing.T) {
	for _, code := range []database.Code{database.Unavailable, database.DeadlineExceeded, database.CommitUnknown} {
		t.Run(string(code), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := database.NewError("transaction", code)
			if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
				t.Fatal("regression requires a driver failure without cancellation identity")
			}
			writer := &shutdownWriter{failure: failure, cancel: cancel}
			p, err := publisher.New(writer, publisher.DefaultConfig(), publisher.Route{Kind: "shutdown", Destination: "jobs", Publish: func(context.Context, publisher.Message) error {
				t.Error("failed transaction published a message")
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Run(ctx); err != nil || writer.calls != 1 {
				t.Fatal("orderly shutdown escaped through a driver failure", err)
			}
			// A one-shot caller still receives the actual failure. Cancellation of
			// the managed loop never converts a publication into confirmed success.
			writer.cancel = nil
			if result, err := p.PublishOne(t.Context()); err != failure || result.Committed || result.Found || writer.calls != 2 {
				t.Fatal("publication failure or released capacity lost")
			}
		})
	}
}
