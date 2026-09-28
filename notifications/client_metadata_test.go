package notifications

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
