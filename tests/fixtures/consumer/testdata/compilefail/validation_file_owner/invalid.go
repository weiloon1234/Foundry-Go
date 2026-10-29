package invalid

import (
	uploads "foundry.test/consumer/httpuploads"
	"github.com/weiloon1234/Foundry-Go/validation"
)

var _ = validation.Rules(uploads.ProfileInputValidationFields().Attachment, validation.MaxLength[string](10))
