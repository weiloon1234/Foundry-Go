package auditqueries

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var Pool = foundation.NewKey[*database.DB]("consumer.audit.database")
var History = foundation.NewKey[*audit.Recorder]("consumer.audit.recorder")
var ErrRejected = errors.New("account rejected after audit recording")

func accountHooks() AccountHooks {
	return AccountHooks{Saved: func(_ context.Context, _ *database.Tx, changes AccountChanges) error {
		after, present := changes.After().Get()
		if present && after.Email == "reject@example.test" {
			return ErrRejected
		}
		return nil
	}}
}
