package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"time"
)

func invalid(id jobs.Name, handler schedule.Handler) {
	_, _ = schedule.Every(id, time.Minute, handler)
}
