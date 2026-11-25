package security

import (
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

// doc-example:begin repository-account-type
// RepositoryAccountID identifies the configured repository owner.
type RepositoryAccountID int64

// doc-example:end repository-account-type

func repositoryVerifier(githubSecret secret.String, account RepositoryAccountID) (*webhook.Verifier[RepositoryAccountID], error) {
	// doc-example:begin webhooks-2
	config := webhook.GitHubConfig("github") // or ShopifyConfig / SlackConfig
	verifier, err := webhook.New(config, account,
		[]secret.String{githubSecret}, clock.System{})
	// doc-example:end webhooks-2
	return verifier, err
}
