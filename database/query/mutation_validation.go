package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func bindAssignment(column columnData, bind func() (driver.Value, error)) (driver.Value, error) {
	return querycore.BindAssignment(column, bind)
}

func (q Query[M]) mutationCompiler(kind mutationKind) (compiler, error) {
	return q.modelWriteCompiler(kind, true)
}

// modelWriteCompiler shares query eligibility while bounded per-model writes
// add a concrete primary identity only after their complete candidate selection.
func (q Query[M]) modelWriteCompiler(kind mutationKind, requirePrimary bool) (compiler, error) {
	if kind >= softDeleteModel && !q.queryCore.HasSoftDeletes() {
		return compiler{}, safeerror.NewFault(fault.Invalid, "special deletion writes require a soft-delete model")
	}
	if err := q.Validate(); err != nil {
		return compiler{}, err
	}
	return q.queryCore.WriteCompiler(kind.SqlKind(), requirePrimary, len(q.relations) != 0 || q.relationLimits != nil)
}

func (q Query[M]) validateMutationShape(kind mutationKind, mutation Mutation[M], c *compiler) (map[string]mutationAssignment, error) {
	return q.queryCore.ValidateMutationShape(kind, mutation.assignments, c)
}

func (q Query[M]) validateMutation(kind mutationKind, mutation Mutation[M], c *compiler) (map[string]mutationAssignment, error) {
	return q.queryCore.ValidateMutation(kind, mutation.assignments, c)
}
