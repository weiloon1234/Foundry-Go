package developeroperations

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/telemetry"
)

// doc-example:begin borrow-telemetry
type TelemetryConsole struct {
	engine *telemetry.Engine
}

func NewTelemetryConsole(services application.Services) (*TelemetryConsole, error) {
	engine, err := services.Telemetry()
	if err != nil {
		return nil, err
	}
	return &TelemetryConsole{engine: engine}, nil
}

func (c *TelemetryConsole) RunID() model.ID[telemetry.Run] { return c.engine.RunID() }

func (c *TelemetryConsole) Traffic(ctx context.Context) (telemetry.Traffic, error) {
	return c.engine.Reader().Traffic(ctx, telemetry.TrafficQuery{Window: telemetry.Window{Minutes: 15}})
}

func (c *TelemetryConsole) Prune(ctx context.Context) (telemetry.PruneResult, error) {
	return c.engine.Prune(ctx)
}

// doc-example:end borrow-telemetry
