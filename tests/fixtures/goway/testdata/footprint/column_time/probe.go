package column_time

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"time"
)

type Model struct {
	ID    int64
	Value time.Time
}
type Fields struct {
	orm.Table[Model, int64]
	Value orm.Time[Model, time.Time]
}

var Models = orm.Define("models", func(b orm.Builder[Model], m *Model) Fields {
	return Fields{Table: orm.TableOf(b, &m.ID), Value: orm.TimeOf(b, &m.Value)}
})
