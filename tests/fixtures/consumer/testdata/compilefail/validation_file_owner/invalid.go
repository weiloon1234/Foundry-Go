package invalid

import (
	uploads "foundry.test/consumer/httpuploads"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = uploads.ProfileInputValidationFields().Attachment.Rules(validation.MaxLength[string](10))
