package invalid

import "foundry.test/consumer/httpkernel"

var _, _ = httpkernel.ShowUser.URL(httpkernel.AssetPath{})
