package invalid

import "foundry.test/consumer/genericdto"

var _ = genericdto.EnvelopeJSON[genericdto.ProjectDTO](genericdto.UserDTOJSON())
