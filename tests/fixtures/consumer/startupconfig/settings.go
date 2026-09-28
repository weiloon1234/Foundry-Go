// Package startupconfig exercises generated deployment configuration without
// defining an application starter or infrastructure factory.
package startupconfig

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type Port uint16

//foundry:enum
type Environment string

const (
	Development Environment = "development"
	Production  Environment = "production"
)

type HTTPSettings struct {
	Listen  string        `config:"listen"`
	Port    Port          `config:"port"`
	Timeout time.Duration `config:"timeout"`
	Origins []string      `config:"origins"`
}

//foundry:config
type Settings struct {
	HTTP        HTTPSettings    `config:"http"`
	Environment Environment     `config:"environment"`
	SigningKey  secret.String   `config:"signing_key"`
	Features    map[string]bool `config:"features"`
}

func Defaults() Settings {
	return Settings{HTTP: HTTPSettings{Listen: "127.0.0.1", Port: 8080, Timeout: 5 * time.Second}, Environment: Development}
}

func Validate(settings Settings) error {
	if settings.HTTP.Port == 0 || settings.HTTP.Timeout <= 0 {
		return fault.New(fault.Invalid, "HTTP port and timeout must be positive")
	}
	return nil
}
