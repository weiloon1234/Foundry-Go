// Package outgoing demonstrates a small domain wrapper around one injected,
// named HTTP client and generated transport DTOs.
package outgoing

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
)

//foundry:dto
type CreateAccount struct {
	Name string `json:"name"`
}

//foundry:dto
type AccountReceipt struct {
	ID   int64  `json:"id,string"`
	Name string `json:"name"`
}

type Accounts struct{ client *httpclient.Client }

func NewAccounts(client *httpclient.Client) *Accounts { return &Accounts{client: client} }
func (s *Accounts) Create(ctx context.Context, name string, token secret.String) (AccountReceipt, error) {
	request, err := httpclient.JSON(ctx, s.client.Post("accounts").Bearer(token), CreateAccountJSON(), CreateAccount{Name: name})
	if err != nil {
		return AccountReceipt{}, err
	}
	response, err := s.client.Do(ctx, request)
	if err != nil {
		return AccountReceipt{}, err
	}
	if err := response.EnsureSuccess(); err != nil {
		return AccountReceipt{}, err
	}
	return httpclient.DecodeJSON(ctx, response, AccountReceiptJSON())
}

func (s *Accounts) Download(ctx context.Context, consume func(context.Context, *httpclient.StreamResponse) error) error {
	return s.client.Stream(ctx, s.client.Get("accounts/export"), consume)
}
