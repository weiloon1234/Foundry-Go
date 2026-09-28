package invalid

import (
	fixture "foundry.test/consumer/teamworkflow"
)

func bad() { fixture.ActionFromQueued(fixture.ProjectView{}) }
