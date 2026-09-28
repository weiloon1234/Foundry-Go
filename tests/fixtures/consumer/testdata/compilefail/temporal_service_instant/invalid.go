package invalid

import "github.com/weiloon1234/Foundry-Go/temporal"

func invalid(dates temporal.Service, date temporal.Date) { _, _ = dates.Format(date) }
