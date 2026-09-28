package invalid

import h "github.com/weiloon1234/Foundry-Go/http"

func wrong(refresh h.RefreshCredential) { _ = h.MFATOTPRequest{Challenge: refresh} }
