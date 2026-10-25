// Package timequeries exercises framework-managed model time in a consumer.
package timequeries

import (
	"errors"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type Member struct {
	ID        model.ID[Member]
	Name      string
	CreatedAt time.Time `foundry:"column=created_on"`
	// UpdatedAt records the last managed model write.
	// Managed update timestamp: ORM writes supply the configured database clock value.
	UpdatedAt temporal.DateTime `foundry:"column=changed_on"`
}

type Manual struct {
	ID        model.ID[Manual]
	CreatedAt time.Time
	UpdatedAt temporal.DateTime
}

var Veto = errors.New("timestamp fixture veto")

func (Member) MutateName(input string) (string, error) {
	if input == "reject" {
		return "", Veto
	}
	return strings.TrimSpace(input), nil
}
