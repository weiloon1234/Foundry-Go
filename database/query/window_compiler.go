package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func groupsSelect(v valueExpression) bool { return querycore.GroupsSelect(v) }

func groupsValue(v valueExpression, depth int) bool { return querycore.GroupsValue(v, depth) }

func groupsCondition(p expression, depth int) bool { return querycore.GroupsCondition(p, depth) }
