package infrastructure

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

//foundry:config
type HTTPClientSettings struct{ Config httpclient.Config }

func DefaultHTTPClientSettings(name httpclient.Name) HTTPClientSettings {
	return HTTPClientSettings{Config: httpclient.DefaultConfig(name)}
}

type HTTPClients map[httpclient.Name]HTTPClientSettings

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

type HTTPClientsSettings struct {
	Default httpclient.Name
	Clients HTTPClients `config:",secret"`
}

func DefaultHTTPClientsSettings() HTTPClientsSettings { return HTTPClientsSettings{Default: "default"} }
