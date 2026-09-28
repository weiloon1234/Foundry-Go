package redis

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	foundry "github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestModuleBuildsIndependentClientsAndOwnsShutdown(t *testing.T) {
	c := transportServer(t, func(conn net.Conn, _ []string, _ <-chan struct{}) { io.WriteString(conn, "+PONG\r\n") })
	key := foundation.NewKey[*Client]("test.redis")
	module := Module("test.redis", key, c)
	var previous *Client
	for range 2 {
		app, err := foundry.New().Register(module).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		client, err := foundation.Resolve(app.Services(), key)
		if err != nil {
			t.Fatal(err)
		}
		if client == previous || client.raw != nil {
			t.Fatal("construction shared or started resources")
		}
		if err := app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := client.Ping(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := app.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-client.Done():
		default:
			t.Fatal("shutdown did not await Redis")
		}
		previous = client
	}
}
func TestModuleCleansUpAfterLaterBootFailure(t *testing.T) {
	c := transportServer(t, func(conn net.Conn, _ []string, _ <-chan struct{}) { io.WriteString(conn, "+PONG\r\n") })
	key := foundation.NewKey[*Client]("test.redis")
	module := Module("test.redis", key, c)
	failure := errors.New("later boot failed")
	dependent := foundation.Module{Name: "test.dependent", Requires: []foundation.ProviderID{module.Name}, OnBoot: func(context.Context, *foundation.Runtime) error { return failure }}
	app, err := foundry.New().Register(dependent, module).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	client, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := app.Shutdown(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	select {
	case <-client.Done():
	default:
		t.Fatal("failed boot left Redis active")
	}
}
