package invalid

import (
	"foundry.test/consumer/inputqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid(w *orm.Write[inputqueries.Member]) {
	var _ value.Optional[value.Nullable[inputqueries.StoredLabel]] = orm.WriteNullableGet(w, func() func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
		receiver := inputqueries.Members.Note
		return func(arg0 inputqueries.LabelInput) orm.Assignment[inputqueries.Member] {
			return orm.NullableInputTextSet(receiver, arg0)
		}
	}())
}
