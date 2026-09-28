package invalid

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func bad() { _ = foundryhttp.CSRFConfig{TrustedOrigins: []string{"https://example.test"}} }
