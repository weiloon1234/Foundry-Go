package s3

import (
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/internal/awscredentials"
)

// WithCredentials selects a borrowed framework provider, shared with other
// disks/mailers. Keep it alive until this adapter has drained and closed.
func (c Config) WithCredentials(provider credentials.Provider) Config {
	if provider == nil {
		c.Credentials = nil
	} else {
		c.Credentials = awscredentials.Adapter{Provider: provider}
	}
	return c
}

// R2WithCredentials supplies explicit credentials without native SDK types.
func R2WithCredentials(bucket, endpoint string, provider credentials.Provider) Config {
	c := DefaultConfig(bucket, "auto")
	c.Provider = R2
	c.Endpoint = endpoint
	c.PathStyle = true
	return c.WithCredentials(provider)
}
