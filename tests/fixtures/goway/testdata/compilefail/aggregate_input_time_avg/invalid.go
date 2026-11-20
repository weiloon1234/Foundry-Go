package aggregate_input_time_avg

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"time"
)

type input struct{ Text string }

var _ = expr.Avg(orm.InputTime[models.User, input, time.Time]{})
