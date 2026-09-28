package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestOutboxRegistrationRejectsDuplicateDestinationsAcrossBuses(t *testing.T) {
	firstBus, secondBus := foundation.NewKey[*events.Bus]("first.bus"), foundation.NewKey[*events.Bus]("second.bus")
	firstOutbox, secondOutbox := foundation.NewKey[*events.Outbox]("first.outbox"), foundation.NewKey[*events.Outbox]("second.outbox")
	for _, differentBus := range []bool{false, true} {
		booted := false
		domain := foundation.Module{Name: "domain", OnRegister: func(r *foundation.Registrar) error {
			if err := events.RegisterOutbox(r, firstOutbox, firstBus, "domain.events"); err != nil {
				return err
			}
			selected := firstBus
			if differentBus {
				selected = secondBus
			}
			return events.RegisterOutbox(r, secondOutbox, selected, "domain.events")
		}, OnBoot: func(context.Context, *foundation.Runtime) error { booted = true; return nil }}
		_, err := foundry.New().Register(
			events.Module("first", firstBus, events.DefaultConfig()),
			events.Module("second", secondBus, events.DefaultConfig()), domain).Build(t.Context())
		if !errors.Is(err, fault.Duplicate) || booted {
			t.Fatal("duplicate durable destination was accepted or started resources", err)
		}
	}
}

func TestOutboxRegistrationKeepsDestinationsAndApplicationInstancesSeparate(t *testing.T) {
	busKey := foundation.NewKey[*events.Bus]("events")
	firstKey, secondKey := foundation.NewKey[*events.Outbox]("first"), foundation.NewKey[*events.Outbox]("second")
	provider := foundation.Module{Name: "domain", Requires: []foundation.ProviderID{"events"}, OnRegister: func(r *foundation.Registrar) error {
		if err := events.RegisterOutbox(r, firstKey, busKey, "first.destination"); err != nil {
			return err
		}
		return events.RegisterOutbox(r, secondKey, busKey, "second.destination")
	}}
	var prior *events.Outbox
	for range 2 {
		app, err := foundry.New().Register(events.Module("events", busKey, events.DefaultConfig()), provider).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		first, err := foundation.Resolve(app.Services(), firstKey)
		if err != nil {
			t.Fatal(err)
		}
		second, err := foundation.Resolve(app.Services(), secondKey)
		if err != nil {
			t.Fatal(err)
		}
		if first == second || first == prior || first.Destination() != "first.destination" || second.Destination() != "second.destination" {
			t.Fatal("outbox registration shared a destination or application instance")
		}
		prior = first
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := app.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
