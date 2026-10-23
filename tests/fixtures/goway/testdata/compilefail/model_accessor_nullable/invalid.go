package model_accessor_nullable

import (
	c "foundry.test/goway/coremodels"
	m "foundry.test/goway/models/mutators"
)

func invalid(v m.Member) c.DisplayLabel { result, _ := v.AccessNote(); return result }
