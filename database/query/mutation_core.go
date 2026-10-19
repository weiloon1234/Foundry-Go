package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func readMutationAssignments(assignments []mutationAssignment) (map[fieldRef]assignmentValue, error) {
	return querycore.ReadMutationAssignments(assignments)
}
