package security

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

// doc-example:begin webhooks-2
// RepositoryAccountID identifies the configured repository owner.
type RepositoryAccountID int64

func repositoryVerifier(githubSecret secret.String) (*webhook.Verifier[RepositoryAccountID], error) {
	config := webhook.GitHubConfig("github") // or ShopifyConfig / SlackConfig
	verifier, err := webhook.New(config, RepositoryAccountID(7),
		[]secret.String{githubSecret}, clock.System{})
	return verifier, err
}

// doc-example:end webhooks-2
