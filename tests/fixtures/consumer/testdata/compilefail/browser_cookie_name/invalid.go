package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func bad() {
	config := foundryhttp.DefaultBrowserSessionConfig()
	config.Cookie = foundryhttp.HeaderName("session")
}
