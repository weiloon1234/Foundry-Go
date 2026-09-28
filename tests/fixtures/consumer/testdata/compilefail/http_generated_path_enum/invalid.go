package invalid

import "foundry.test/consumer/httpkernel"

var state string
var _ = httpkernel.UserFeedPath{Status: state}
