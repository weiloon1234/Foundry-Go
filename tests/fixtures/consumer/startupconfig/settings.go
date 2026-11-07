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

// doc-example:begin deployment-settings

type HTTPSettings struct {
	Listen  string        `config:"listen"`
	Port    Port          `config:"port"` // Port is the public listener port.
	Timeout time.Duration `config:"timeout"`
	Origins []string      `config:"origins"`
}

//foundry:config
type Settings struct {
	// HTTP controls the public listener and its request timeout.
	HTTP        HTTPSettings    `config:"http"`
	Environment Environment     `config:"environment"`
	SigningKey  secret.String   `config:"signing_key"`
	Features    map[string]bool `config:"features"`
}

// doc-example:end deployment-settings

// doc-example:begin configuration-defaults
func Defaults() Settings {
	return Settings{HTTP: HTTPSettings{Listen: "127.0.0.1", Port: 8080, Timeout: 5 * time.Second}, Environment: Development}
}

// doc-example:end configuration-defaults

// doc-example:begin configuration-validation
func Validate(settings Settings) error {
	if settings.HTTP.Port == 0 || settings.HTTP.Timeout <= 0 {
		return fault.New(fault.Invalid, "HTTP port and timeout must be positive")
	}
	return nil
}

// doc-example:end configuration-validation
