package background

import (
	"context"
	"time"

	"foundry.test/consumer/outgoing"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// doc-example:begin named-client-deadline
const AccountsClient httpclient.Name = "accounts.api"

type AccountService struct{ accounts *outgoing.Accounts }

func NewAccountService(services application.Services) (*AccountService, error) {
	client, err := services.HTTPClients.Client(AccountsClient)
	if err != nil {
		return nil, err
	}
	return &AccountService{accounts: outgoing.NewAccounts(client)}, nil
}

func (s *AccountService) Create(ctx context.Context, name string, token secret.String) (outgoing.AccountReceipt, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.accounts.Create(bounded, name, token)
}

// doc-example:end named-client-deadline
