package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/redis"
	"github.com/weiloon1234/Foundry-Go/secret"
	"time"
)

// RedisConnectionSettings addresses one standalone server, with verified TLS by
// default. Custom TLS certificates use the advanced redis.Config/module API.
//
//foundry:config
type RedisConnectionSettings struct {
	Host                                                                    string
	Port                                                                    uint16
	Database                                                                int
	User                                                                    string
	Password                                                                secret.String
	TLS                                                                     redis.TLSMode
	MaxConnections, MaxOperations, MaxSubscriptions                         int
	ConnectTimeout, OperationTimeout, PoolTimeout, MaxIdleTime, MaxLifetime time.Duration
	MaxValueBytes                                                           int
}

func RedisSettingsFromConfig(c redis.Config) RedisConnectionSettings {
	return RedisConnectionSettings{Host: c.Host, Port: c.Port, Database: c.Database, User: c.User, Password: c.Password, TLS: c.TLS, MaxConnections: c.MaxConnections, MaxOperations: c.MaxOperations, MaxSubscriptions: c.MaxSubscriptions, ConnectTimeout: c.ConnectTimeout, OperationTimeout: c.OperationTimeout, PoolTimeout: c.PoolTimeout, MaxIdleTime: c.MaxIdleTime, MaxLifetime: c.MaxLifetime, MaxValueBytes: c.MaxValueBytes}
}
func DefaultRedisConnectionSettings() RedisConnectionSettings {
	return RedisSettingsFromConfig(redis.DefaultConfig())
}
func (s RedisConnectionSettings) Config() redis.Config {
	return redis.Config{Host: s.Host, Port: s.Port, Database: s.Database, User: s.User, Password: s.Password, TLS: s.TLS, MaxConnections: s.MaxConnections, MaxOperations: s.MaxOperations, MaxSubscriptions: s.MaxSubscriptions, ConnectTimeout: s.ConnectTimeout, OperationTimeout: s.OperationTimeout, PoolTimeout: s.PoolTimeout, MaxIdleTime: s.MaxIdleTime, MaxLifetime: s.MaxLifetime, MaxValueBytes: s.MaxValueBytes}
}
func (s RedisConnectionSettings) Validate() error { return s.Config().Validate() }

type RedisConnections map[redis.ConnectionName]RedisConnectionSettings

func (m *RedisConnections) UnmarshalText(data []byte) error {
	schema, err := RedisConnectionSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(redis.ConnectionName) RedisConnectionSettings { return DefaultRedisConnectionSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

type RedisSettings struct {
	Default        redis.ConnectionName
	MaxConnections int
	Connections    RedisConnections `config:",secret"`
}

func DefaultRedisSettings() RedisSettings {
	return RedisSettings{Default: "default", MaxConnections: 256}
}
