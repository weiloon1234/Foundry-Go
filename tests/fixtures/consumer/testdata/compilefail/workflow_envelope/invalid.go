package invalid

import (
	"foundry.test/consumer/genericdto"
	fixture "foundry.test/consumer/teamworkflow"
)

var _ fixture.ActionEnvelope = genericdto.Envelope[fixture.ProjectView]{}
