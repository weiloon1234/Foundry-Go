package invalid

import "foundry.test/consumer/catalog"

var invalid = catalog.ProductFields().Status.Like("available%")
