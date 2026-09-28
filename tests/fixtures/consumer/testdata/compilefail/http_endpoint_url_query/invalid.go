package invalid

import (
	"context"
	"foundry.test/consumer/httpendpoints"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpquery"
)

var _, _ = httpendpoints.Update.URL(context.Background(), httpkernel.UserPath{}, httpquery.OtherInput{})
