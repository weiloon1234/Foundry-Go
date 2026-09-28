package ses

import (
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/internal/awscredentials"
)

// WithCredentials borrows a framework provider shared with other cloud adapters.
func (c Config) WithCredentials(provider credentials.Provider) Config {
	if provider == nil {
		c.Credentials = nil
	} else {
		c.Credentials = awscredentials.Adapter{Provider: provider}
	}
	return c
}
