package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestRedisInventoryBeforeApplicationBuild(t *testing.T) {
	settings := application.DefaultSettings()
	settings.Services.Namespace = keyspace.Namespace{Application: "consumer", Environment: "production"}
	connection := infrastructure.DefaultRedisConnectionSettings()
	connection.Host = "unreachable.invalid"
	connection.Password = secret.New("never-in-inventory")
	settings.Services.Redis.Connections = infrastructure.RedisConnections{"default": connection}
	limits := foundation.NewKey[*ratelimit.Store]("consumer.limits")
	protection := foundation.NewKey[*lockout.Store]("consumer.protection")
	builder := application.New(settings,
		application.WithRedisStores(
			infrastructure.RedisRateLimitStore(limits, "", ratelimit.DefaultConfig(keyspace.Namespace{})),
			infrastructure.RedisLockoutStore(protection, "", lockout.DefaultConfig(keyspace.Namespace{})),
		),
		application.WithCredentials("default", credentials.ProviderFunc(func(context.Context) (credentials.Value, error) { panic("inspection retrieved credentials") })),
	).Register(foundation.Module{Name: "must-not-run", OnRegister: func(*foundation.Registrar) error { panic("inspection ran an application provider") }})
	inventory, err := builder.RedisKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 1 || len(inventory[0].Keys) != 2 {
		t.Fatal(inventory)
	}
	command, err := application.RedisKeysCommand("redis.keys", builder)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := cli.New(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"redis.keys"}, {"redis.keys", "--json"}} {
		var output strings.Builder
		invocation, err := registry.Parse(args, &output)
		if err != nil {
			t.Fatal(err)
		}
		if err := invocation.Run(t.Context(), nil, cli.Streams{In: strings.NewReader(""), Out: &output, Err: &output}); err != nil {
			t.Fatal(err)
		}
		wire := output.String()
		if strings.Contains(wire, "never-in-inventory") || strings.Contains(wire, "unreachable") {
			t.Fatal("inspection exposed private configuration")
		}
		for _, prefix := range []string{"foundry:ratelimit:v1:consumer:production:", "foundry:lockout:v1:consumer:production:"} {
			if !strings.Contains(wire, prefix) {
				t.Fatal("missing prefix", prefix)
			}
		}
		if len(args) == 2 {
			var decoded []infrastructure.RedisConnectionInventory
			if err := json.Unmarshal([]byte(wire), &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded) != 1 || len(decoded[0].Keys) != 2 {
				t.Fatal(decoded)
			}
		}
	}
}
