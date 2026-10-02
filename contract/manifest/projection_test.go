package manifest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
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
	// Path prefixes select whole segments and combine with ID namespaces.
	byPath := project(t, source, manifest.Selection{Paths: []string{"/admin"}})
	if !slices.Equal(routeIDs(byPath), []string{"admin", "admin.orders.list"}) {
		t.Fatal("path prefix selection", routeIDs(byPath))
	}
	combined := project(t, source, manifest.Selection{Routes: []foundryhttp.RouteID{"web.login"}, Paths: []string{"/profile", "/admin/orders"}})
	if !slices.Equal(routeIDs(combined), []string{"admin.orders.list", "web.login", "web.profile"}) {
		t.Fatal("combined selection", routeIDs(combined))
	}
	if every := project(t, source, manifest.Selection{Paths: []string{"/"}}); len(every.HTTP) != 5 {
		t.Fatal("root path prefix", routeIDs(every))
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
		"overlapping path": {manifest.Selection{Routes: []foundryhttp.RouteID{"admin"}, Paths: []string{"/admin"}}, nil},
		"partial segment":  {manifest.Selection{Paths: []string{"/adm"}}, fault.Missing},
		"unknown path":     {manifest.Selection{Paths: []string{"/merchant"}}, fault.Missing},
		"repeated path":    {manifest.Selection{Paths: []string{"/admin", "/admin"}}, fault.Duplicate},
		"trailing slash":   {manifest.Selection{Paths: []string{"/admin/"}}, fault.Invalid},
		"parameter":        {manifest.Selection{Paths: []string{"/admin/{id}"}}, fault.Invalid},
		"relative path":    {manifest.Selection{Paths: []string{"admin"}}, fault.Invalid},
		"dot segment":      {manifest.Selection{Paths: []string{"/admin/.."}}, fault.Invalid},
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

type operator struct{ ID int64 }

func (o operator) FoundryReference() model.Reference[operator, int64] {
	return model.NewReference[operator]("operators", o.ID, codec.Signed[int64]())
}
func (o operator) FoundryIdentity() (model.Identity, error) { return o.FoundryReference().Identity() }

func operatorGuard(name auth.GuardName) auth.Guard[operator] {
	provider := auth.DefineProvider(auth.ProviderName(name+".operators"), operator{}.FoundryReference(), func(_ context.Context, id int64) (value.Optional[operator], error) {
		return value.Set(operator{id}), nil
	}, func(context.Context, operator) (bool, error) { return true, nil })
	strategy := auth.DefineStrategy(auth.CredentialName(name+".bearer"), func(context.Context, secret.String) (value.Optional[auth.Proof[operator, int64]], error) {
		return value.Optional[auth.Proof[operator, int64]]{}, nil
	})
	return auth.DefineGuard(name, provider, strategy)
}

// guardedPortals has routes guarded by admin.api and billing.api, a public
// route, private channels for admin.api and staff.api, and a public channel.
// billing.api guards a route but no channel; staff.api a channel but no route.
func guardedPortals(t *testing.T) *manifest.Manifest {
	t.Helper()
	admin, staff, billing := operatorGuard("admin.api"), operatorGuard("staff.api"), operatorGuard("billing.api")
	guarded := func(id foundryhttp.RouteID, path string, guard auth.Guard[operator]) foundryhttp.RouteRegistration {
		registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
		if err != nil {
			t.Fatal(err)
		}
		transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.BearerCredential(guard.Source()))
		if err != nil {
			t.Fatal(err)
		}
		endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath(path)), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]()))
		return foundryhttp.RequireAuthentication(endpoint, transport, guard).Handle(func(context.Context, operator, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
			t.Fatal("metadata invoked handler")
			return "", nil
		})
	}
	router, err := foundryhttp.NewRouter(guarded("admin.orders", "/admin/orders", admin), guarded("billing.invoices", "/billing/invoices", billing), endpoint(t, "web.home", "/home"))
	if err != nil {
		t.Fatal(err)
	}
	rooms := websocket.DefineRooms(foundryhttp.StringPath[string]())
	allow := func(context.Context, operator, websocket.Target[string]) error { return nil }
	adminFeed := websocket.Private[struct{}]("admin.feed", rooms, admin, allow)
	staffFeed := websocket.Private[struct{}]("staff.feed", rooms, staff, allow)
	news := websocket.Public[struct{}]("news", rooms)
	registry, err := websocket.NewRegistry(
		websocket.Register(adminFeed, websocket.DefineOutgoing(adminFeed, "posted", contract.StringJSON[string]()).Registration()),
		websocket.Register(staffFeed, websocket.DefineOutgoing(staffFeed, "posted", contract.StringJSON[string]()).Registration()),
		websocket.Register(news, websocket.DefineOutgoing(news, "posted", contract.StringJSON[string]()).Registration()),
	)
	if err != nil {
		t.Fatal(err)
	}
	realtime, err := websocket.DescribeClient(registry, websocket.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router, Realtime: &realtime})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func channelIDs(document manifest.Document) []string {
	var ids []string
	if document.Realtime != nil {
		for _, channel := range document.Realtime.Channels {
			ids = append(ids, string(channel.ID))
		}
	}
	slices.Sort(ids)
	return ids
}

func TestProjectionSelectsTheChannelsOfAGuard(t *testing.T) {
	source := guardedPortals(t)
	admin := project(t, source, manifest.Selection{Paths: []string{"/admin"}, Guards: []auth.GuardName{"admin.api"}})
	if !slices.Equal(routeIDs(admin), []string{"admin.orders"}) || !slices.Equal(channelIDs(admin), []string{"admin.feed"}) {
		t.Fatal("admin portal", routeIDs(admin), channelIDs(admin))
	}
	// A guard combines with explicit channels; public channels have no guard.
	staff := project(t, source, manifest.Selection{Channels: []websocket.ChannelID{"news"}, Guards: []auth.GuardName{"staff.api"}})
	if len(staff.HTTP) != 0 || !slices.Equal(channelIDs(staff), []string{"news", "staff.feed"}) {
		t.Fatal("staff portal", routeIDs(staff), channelIDs(staff))
	}
	// A known guard without channels selects none; the portal still exports.
	billing := project(t, source, manifest.Selection{Paths: []string{"/billing"}, Guards: []auth.GuardName{"billing.api"}})
	if !slices.Equal(routeIDs(billing), []string{"billing.invoices"}) || billing.Realtime != nil {
		t.Fatal("billing portal", routeIDs(billing), channelIDs(billing))
	}
	for name, test := range map[string]struct {
		selection manifest.Selection
		want      error
	}{
		"unknown guard":   {manifest.Selection{Paths: []string{"/admin"}, Guards: []auth.GuardName{"merchant.api"}}, fault.Missing},
		"selects nothing": {manifest.Selection{Guards: []auth.GuardName{"billing.api"}}, fault.Missing},
		"repeated guard":  {manifest.Selection{Guards: []auth.GuardName{"admin.api", "admin.api"}}, fault.Duplicate},
		"invalid guard":   {manifest.Selection{Guards: []auth.GuardName{"not semantic"}}, fault.Invalid},
	} {
		if _, err := source.Project(test.selection); !errors.Is(err, test.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
