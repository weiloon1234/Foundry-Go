package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestNotificationDeclarationIdentityAndCaptureBoundaries(t *testing.T) {
	a := newAuthority(t)
	d := Define("notice", 1, textSchema[Input]())
	c := databaseChannel()
	b := Bind(d, a.recipient, c.Channel())
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(b.Registration(), b.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate binding", err)
	}
	if err := Bind(d, a.recipient, c.Channel(), c.Channel()).Validate(); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate channel", err)
	}
	other := auth.DefineProvider("notification.members", Member{}.FoundryReference(), func(context.Context, int64) (value.Optional[Member], error) { return value.Optional[Member]{}, nil }, func(context.Context, Member) (bool, error) { return true, nil })
	wrong := DefineRecipient("members", other, a.guard, func(context.Context, Member, Name, ChannelID) (bool, error) { return true, nil })
	if err := wrong.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign provider declaration accepted", err)
	}
	for _, invalid := range []Binding[Member, int64, Input]{{}, Bind(d, a.recipient), Bind(Definition[Input]{}, a.recipient, c.Channel())} {
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	p := captureNotification(t, b, 1, "private")
	if _, err := json.Marshal(p); err == nil {
		t.Fatal("runtime pending handle serialized")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := b.Capture(ctx, Member{ID: 1}.FoundryReference(), Input{"x"}, ID[Member]{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := New(nil, nil, DefaultConfig()); err == nil {
		t.Fatal("nil database accepted")
	}
}
