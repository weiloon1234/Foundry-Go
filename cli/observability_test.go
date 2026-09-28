package cli_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/tracing"
)

func TestCLIUsesSharedObservationAndMaintenanceBeforeConstruction(t *testing.T) {
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close(context.Background())
	constructed, called := 0, 0
	declaration, err := command().Declare(func(foundation.Resolver) (cli.Handler[arguments], error) {
		constructed++
		return func(ctx context.Context, _ arguments, _ cli.Streams) error {
			called++
			if observability.FromContext(ctx) != recorder || tracing.FromContext(ctx).IsZero() {
				return errors.New("missing CLI observation")
			}
			return nil
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(declaration)
	if err != nil {
		t.Fatal(err)
	}
	ctx := observability.WithContext(t.Context(), recorder)
	if err := recorder.Gate().Set(true); err != nil {
		t.Fatal(err)
	}
	invocation, err := registry.Parse([]string{"greet", "--name", "Ada"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.Run(ctx, nil, streams(io.Discard)); !errors.Is(err, maintenance.ErrMaintenance) || constructed != 0 {
		t.Fatal("maintenance constructed a command service", err)
	}
	if err := recorder.Gate().Set(false); err != nil {
		t.Fatal(err)
	}
	invocation, err = registry.Parse([]string{"greet", "--name", "Ada"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.Run(ctx, nil, streams(io.Discard)); err != nil {
		t.Fatal(err)
	}
	snapshot := recorder.Snapshot()
	if constructed != 1 || called != 1 || snapshot.Completed != 2 || snapshot.Recent[0].Result.Outcome != observability.Rejected || snapshot.Recent[1].Result.Outcome != observability.Succeeded {
		t.Fatal("CLI admission observations incorrect", snapshot)
	}
}
