package application

import (
	"context"
	"encoding/json"
	"io"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage/probe"
)

// ProbeStorage boots only storage infrastructure, reports every disk's deployment
// probe, and shuts it down. It leaves the builder available for application assembly.
func ProbeStorage(ctx context.Context, builder *Builder, output io.Writer) (err error) {
	if builder == nil || builder.state == nil {
		return safeerror.NewFault(fault.Invalid, "storage probe requires an application builder")
	}
	builder.state.mu.Lock()
	if builder.state.err != nil {
		err = builder.state.err
		builder.state.mu.Unlock()
		return err
	}
	plan, err := infrastructure.ConfigureStorage(builder.state.settings.Services, builder.state.options.infrastructure...)
	builder.state.mu.Unlock()
	if err != nil {
		return safeerror.Wrap("storage probe configuration failed", err)
	}
	app, err := foundation.NewBuilder().Register(plan.Providers()...).Build(ctx)
	if err != nil {
		return safeerror.Wrap("storage probe application build failed", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), app.ShutdownTimeout())
		defer cancel()
		err = safeerror.Join("storage probe failed", err,
			safeerror.Wrap("storage probe application shutdown failed", app.Shutdown(shutdownCtx)))
	}()
	if err := app.Start(ctx); err != nil {
		return safeerror.Wrap("storage probe application startup failed", err)
	}
	services, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
	if err != nil {
		return safeerror.Wrap("storage probe services unavailable", err)
	}
	report := struct {
		Schema int            `json:"schema"`
		Disks  []probe.Result `json:"disks"`
	}{Schema: 1, Disks: []probe.Result{}}
	registry := services.Storage
	for _, id := range registry.Disks() {
		disk, resolveErr := registry.Disk(id)
		result := probe.Result{Disk: id, Outcome: "failed", Stage: "write", Code: "storage_failed"}
		if resolveErr == nil {
			result = probe.Disk(ctx, disk)
		}
		report.Disks = append(report.Disks, result)
		if result.Outcome != "ok" && err == nil {
			err = safeerror.NewFault(fault.Internal, "storage probe failed; check the disk report")
		}
	}
	return safeerror.Join("storage probe failed", err,
		safeerror.Wrap("storage probe report write failed", json.NewEncoder(output).Encode(report)))
}
