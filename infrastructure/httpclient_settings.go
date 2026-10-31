package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

// HTTPClientSettings contains configuration for a named HTTP client.
//
//foundry:config
type HTTPClientSettings struct{ Config httpclient.Config }

// DefaultHTTPClientSettings returns default HTTP client settings for the supplied name.
func DefaultHTTPClientSettings(name httpclient.Name) HTTPClientSettings {
	return HTTPClientSettings{Config: httpclient.DefaultConfig(name)}
}

// HTTPClients maps HTTP client names to settings.
type HTTPClients map[httpclient.Name]HTTPClientSettings

// UnmarshalText decodes HTTP client settings, replacing the receiver only on success.
func (m *HTTPClients) UnmarshalText(data []byte) error {
	schema, err := HTTPClientSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, DefaultHTTPClientSettings, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

// HTTPClientsSettings configures named HTTP clients and their default.
type HTTPClientsSettings struct {
	Default httpclient.Name
	Clients HTTPClients `config:",secret"`
}

// DefaultHTTPClientsSettings returns HTTP client settings selecting the default client name.
func DefaultHTTPClientsSettings() HTTPClientsSettings { return HTTPClientsSettings{Default: "default"} }
