package mailing

import "github.com/weiloon1234/Foundry-Go/email"

// RecipientRequest demonstrates the same address value in a generated transport
// DTO. Only schema metadata is generated; email owns parsing and validation.
//
//foundry:dto
type RecipientRequest struct {
	Recipient email.Address `json:"recipient"`
}
