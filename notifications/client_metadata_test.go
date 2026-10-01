package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestNotificationClientMetadataExcludesPrivateInputsAndTransports(t *testing.T) {
	a := newAuthority(t)
	render := func(context.Context, Member, DeliveryContext, Input) (InboxData, error) {
		t.Fatal("metadata ran renderer")
		return InboxData{}, nil
	}
	public := Database("inbox", textSchema[InboxData](), render)
	private := Custom("private.transport", textSchema[Input](), func(context.Context, Member, DeliveryContext, Input) (Input, error) {
		t.Fatal("private renderer ran")
		return Input{}, nil
	}, transportFunc[Input](func(context.Context, DeliveryID, Input) (Outcome, error) {
		t.Fatal("transport ran")
		return Unknown, nil
	}))
	registry, err := NewRegistry(Bind(Define("notice", 1, textSchema[Input]()), a.recipient, private, public.Channel()).Registration())
	if err != nil {
		t.Fatal(err)
	}
	info, err := registry.ClientDescriptions()
	if err != nil || len(info) != 1 || len(info[0].Channels) != 1 || info[0].Channels[0].ID != "inbox" {
		t.Fatal("wrong public metadata", err)
	}
	data, err := json.Marshal(info)
	if err != nil || strings.Contains(string(data), ".Input") || strings.Contains(string(data), "private.transport") {
		t.Fatal("private payload exported")
	}
	info[0].Channels[0].Payload.Types[0].ID = "changed"
	again, err := registry.ClientDescriptions()
	if err != nil || again[0].Channels[0].Payload.Types[0].ID == "changed" {
		t.Fatal("metadata aliases registry")
	}
	if _, err := (*Registry)(nil).ClientDescriptions(); err == nil {
		t.Fatal("nil registry accepted")
	}
}

// Inbox and realtime payloads are output: one reaching a password hint fails
// registration, whether or not clients are exported. Notification inputs and
// private transport payloads are not client output.
func TestNotificationOutputsRejectPasswordPresentation(t *testing.T) {
	a := newAuthority(t)
	render := func(context.Context, Member, DeliveryContext, Input) (InboxData, error) { return InboxData{}, nil }
	inbox := Database("inbox", passwordSchema[InboxData](), render)
	if _, err := NewRegistry(Bind(Define("notice", 1, textSchema[Input]()), a.recipient, inbox.Channel()).Registration()); !errors.Is(err, fault.Invalid) {
		t.Fatal("password inbox payload registered", err)
	}
	private := Custom("private.transport", passwordSchema[InboxData](), render, transportFunc[InboxData](func(context.Context, DeliveryID, InboxData) (Outcome, error) {
		return Accepted, nil
	}))
	if _, err := NewRegistry(Bind(Define("notice", 1, passwordSchema[Input]()), a.recipient, private).Registration()); err != nil {
		t.Fatal("password input or private transport rejected", err)
	}
}

func passwordSchema[T any]() contract.JSON[T] {
	typ := reflect.TypeFor[T]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[T](contract.Schema{Root: id, Types: []contract.Type{
		{ID: id, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "text", Type: "string", Required: true, Presentation: contract.Presentation{Kind: contract.PasswordPresentation}}}},
		{ID: "string", Kind: contract.StringKind},
	}})
}
