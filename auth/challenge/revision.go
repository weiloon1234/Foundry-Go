package challenge

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// RevisionOwner keeps a recovery-state generation separate from a model's primary
// key, while retaining its model owner. It is a type marker, not a stored model.
type RevisionOwner[M any] struct{ _ [0]*M }

// Revision is a persisted recovery-state generation, using the existing typed
// UUID codec. It is not a secret, account ID or authentication capability. Keep
// it on the model and replace it transactionally when protected state changes;
// never restore an earlier revision when restoring an earlier field value.
type Revision[M any] = model.ID[RevisionOwner[M]]

func NewRevision[M any]() (Revision[M], error) { return model.NewID[RevisionOwner[M]]() }

// BindRevision binds current values AND their persisted history generation.
// A zero generation fails closed; consumers must initialize existing rows before
// enabling a recovery flow. The revision remains independent of link issuance.
func BindRevision[M any](revision Revision[M], parts ...secret.String) (Binding, error) {
	if len(parts) < 1 || len(parts) > 7 {
		return Binding{}, fault.New(fault.Invalid, "invalid revision binding components")
	}
	if revision.IsZero() {
		return Binding{}, fault.New(fault.Invalid, "recovery state requires a persisted revision")
	}
	values := make([]secret.String, 0, len(parts)+1)
	values = append(values, secret.New(revision.String()))
	values = append(values, parts...)
	return Bind(values...)
}
