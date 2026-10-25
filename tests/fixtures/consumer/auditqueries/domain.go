package auditqueries

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

var Pool = foundation.NewKey[*database.DB]("consumer.audit.database")
var History = foundation.NewKey[*audit.Recorder]("consumer.audit.recorder")
var ErrRejected = errors.New("account rejected after audit recording")
