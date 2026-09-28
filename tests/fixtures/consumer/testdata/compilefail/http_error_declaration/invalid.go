package invalid

import (
	"errors"
	"foundry.test/consumer/httpendpoints"
)

var _ = httpendpoints.Reserve.WithErrors(errors.New("unsafe public text"))
