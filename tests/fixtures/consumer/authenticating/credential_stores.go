package authenticating

import (
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	"github.com/weiloon1234/Foundry-Go/redis"
)

// doc-example:begin credential-stores
var CredentialLimitsKey = foundation.NewKey[*ratelimit.Store]("credentials.limits")
var CredentialProtectionKey = foundation.NewKey[*lockout.Store]("credentials.protection")

func CredentialStores(connection redis.ConnectionName) application.Option {
	return application.WithRedisStores(
		infrastructure.RedisRateLimitStore(CredentialLimitsKey, connection, ratelimit.DefaultConfig(keyspace.Namespace{})),
		infrastructure.RedisLockoutStore(CredentialProtectionKey, connection, lockout.DefaultConfig(keyspace.Namespace{})),
	)
}

// doc-example:end credential-stores

// doc-example:begin credential-store-inventory
func CredentialStoreInventory(settings application.Settings, connection redis.ConnectionName) ([]infrastructure.RedisConnectionInventory, error) {
	return application.New(settings, CredentialStores(connection)).RedisKeys()
}

// doc-example:end credential-store-inventory

// doc-example:begin password-lockout
var PasswordAttempts = lockout.DefineLogin("password.accounts", keyspace.StringKeys[string](), lockout.DefaultLimits())

func ProtectPasswordLogin(resolver foundation.Resolver, login *passwords.Login) (*passwords.Login, error) {
	store, err := foundation.Resolve(resolver, CredentialProtectionKey)
	if err != nil {
		return nil, err
	}
	throttle, err := PasswordAttempts.Bind(store)
	if err != nil {
		return nil, err
	}
	return login.WithLockout(throttle)
}

// doc-example:end password-lockout
