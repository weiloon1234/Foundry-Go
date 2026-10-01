package manifest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// portals is a manifest with admin and web routes, two channels, a public root
// schema and a notification delivered to an inbox and over the orders channel.
func portals(t *testing.T) *manifest.Manifest {
	t.Helper()
	router, err := foundryhttp.NewRouter(endpoint(t, "admin", "/admin"), endpoint(t, "admin.orders.list", "/admin/orders"), endpoint(t, "administration.audit", "/audit"), endpoint(t, "web.profile", "/profile"), endpoint(t, "web.login", "/login"))
	if err != nil {
		t.Fatal(err)
	}
	orders := websocket.Public[struct{}]("orders", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	chat := websocket.Public[struct{}]("chat", websocket.DefineRooms(foundryhttp.StringPath[string]()))
	registry, err := websocket.NewRegistry(websocket.Register(orders, websocket.DefineOutgoing(orders, "updated", contract.StringJSON[string]()).Registration()), websocket.Register(chat, websocket.DefineOutgoing(chat, "posted", contract.StringJSON[string]()).Registration()))
	if err != nil {
		t.Fatal(err)
	}
	realtime, err := websocket.DescribeClient(registry, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	public := contract.Schema{Root: "test.Public", Types: []contract.Type{{ID: "test.Public", Kind: contract.StringKind}}}
	built, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Realtime: &realtime, Schemas: []contract.Schema{public}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := built.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	object := func(id contract.TypeID, property string) contract.Type {
		return contract.Type{ID: id, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: property, Type: "string", Required: true}}}
	}
	document.Types = append(document.Types, object("test.AdminRow", "name"), object("test.WebProfile", "display"), object("test.Notice", "text"))
	for i := range document.HTTP {
		switch document.HTTP[i].Route.ID {
		case "admin.orders.list":
			document.HTTP[i].Response.Type = "test.AdminRow"
		case "web.profile":
			document.HTTP[i].Response.Type = "test.WebProfile"
		}
	}
	document.Notifications = []manifest.Notification{{Recipient: "admins", Name: "order.created", Version: 1, Channels: []manifest.NotificationChannel{
		{ID: "inbox", Kind: "database", Payload: "test.Notice"},
		{ID: "live", Kind: "realtime", Payload: "string", Realtime: &notifications.ClientRealtimeInfo{Channel: "orders", Event: "updated"}},
	}}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func project(t *testing.T, source *manifest.Manifest, selection manifest.Selection) manifest.Document {
	t.Helper()
	projected, err := source.Project(selection)
	if err != nil {
		t.Fatal(err)
	}
	data, err := projected.JSON()
	if err != nil {
		t.Fatal(err)
	}
	// A projection is an ordinary canonical manifest.
	decoded, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decoded.JSON()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("projection is not canonical", err)
	}
	document, err := projected.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func routeIDs(document manifest.Document) []string {
	var ids []string
	for _, op := range document.HTTP {
		ids = append(ids, string(op.Route.ID))
	}
	slices.Sort(ids)
	return ids
}

func typeIDs(document manifest.Document) map[contract.TypeID]bool {
	ids := make(map[contract.TypeID]bool)
	for _, typ := range document.Types {
		ids[typ.ID] = true
	}
	return ids
}

func TestProjectionSelectsNamespacesAndTheirReachableContract(t *testing.T) {
	source := portals(t)
	before, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}

	admin := project(t, source, manifest.Selection{Routes: []foundryhttp.RouteID{"admin"}})
	if !slices.Equal(routeIDs(admin), []string{"admin", "admin.orders.list"}) || admin.Realtime != nil {
		t.Fatal("admin namespace selection", routeIDs(admin), admin.Realtime)
	}
	types := typeIDs(admin)
	if !types["test.AdminRow"] || types["test.WebProfile"] || !types["test.Public"] || !types[admin.ErrorType] {
		t.Fatal("admin types do not follow its operations, roots and errors", types)
	}
	// The inbox delivery stays; the orders delivery has no selected channel.
	if len(admin.Notifications) != 1 || len(admin.Notifications[0].Channels) != 1 || admin.Notifications[0].Channels[0].ID != "inbox" || !types["test.Notice"] {
		t.Fatal("admin notification deliveries", admin.Notifications)
	}

	web := project(t, source, manifest.Selection{Routes: []foundryhttp.RouteID{"web"}, Channels: []websocket.ChannelID{"orders"}})
	if !slices.Equal(routeIDs(web), []string{"web.login", "web.profile"}) || web.Realtime == nil || len(web.Realtime.Channels) != 1 || web.Realtime.Channels[0].ID != "orders" {
		t.Fatal("web selection", routeIDs(web), web.Realtime)
	}
	types = typeIDs(web)
	if types["test.AdminRow"] || !types["test.WebProfile"] || len(web.Notifications) != 1 || len(web.Notifications[0].Channels) != 2 {
		t.Fatal("web types or notification deliveries", types, web.Notifications)
	}

	// A channel-only surface needs no operation.
	chat := project(t, source, manifest.Selection{Channels: []websocket.ChannelID{"chat"}})
	if len(chat.HTTP) != 0 || chat.Realtime == nil || chat.Realtime.Channels[0].ID != "chat" {
		t.Fatal("channel-only selection", chat)
	}
	// An exact ID selects only itself.
	audit := project(t, source, manifest.Selection{Routes: []foundryhttp.RouteID{"administration.audit"}})
	if !slices.Equal(routeIDs(audit), []string{"administration.audit"}) {
		t.Fatal("exact selection", routeIDs(audit))
	}
	after, err := source.JSON()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("projection changed its source", err)
	}
}

func TestProjectionRejectsUnknownRepeatedAndEmptySelections(t *testing.T) {
	source := portals(t)
	for name, test := range map[string]struct {
		selection manifest.Selection
		want      error
	}{
		"empty":            {manifest.Selection{}, fault.Invalid},
		"unknown route":    {manifest.Selection{Routes: []foundryhttp.RouteID{"merchant"}}, fault.Missing},
		"unknown channel":  {manifest.Selection{Routes: []foundryhttp.RouteID{"admin"}, Channels: []websocket.ChannelID{"missing"}}, fault.Missing},
		"partial name":     {manifest.Selection{Routes: []foundryhttp.RouteID{"adm"}}, fault.Missing},
		"repeated":         {manifest.Selection{Routes: []foundryhttp.RouteID{"admin", "admin"}}, fault.Duplicate},
		"invalid":          {manifest.Selection{Routes: []foundryhttp.RouteID{"not semantic"}}, fault.Invalid},
		"overlap is valid": {manifest.Selection{Routes: []foundryhttp.RouteID{"admin", "admin.orders"}}, nil},
	} {
		_, err := source.Project(test.selection)
		if test.want == nil && err != nil || test.want != nil && !errors.Is(err, test.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var missing *manifest.Manifest
	if _, err := missing.Project(manifest.Selection{Routes: []foundryhttp.RouteID{"admin"}}); err == nil {
		t.Fatal("uninitialized manifest projected")
	}
}
