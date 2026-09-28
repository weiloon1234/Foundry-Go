package invalid

import "github.com/weiloon1234/Foundry-Go/httpclient"

func invalid(r httpclient.Request) { _ = r.WithBody([]byte("body")) }
