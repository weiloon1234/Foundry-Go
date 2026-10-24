package expr

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Fixed literal codecs belong outside generic shells. These private arguments
// are consumed only by operations whose typed operand establishes ownership.
func scalarParameter(kind codec.ParameterType, v driver.Value, err error) ormcore.OperationArgument {
	return ormcore.OperationArgument{Kind: kind, Value: ormcore.Expression{Node: querycore.ParameterNode{Kind: kind, Value: v, Err: err}, ReferenceFree: true, Err: err}}
}
func integerArgument(value int32) ormcore.OperationArgument {
	v, err := codec.Signed[int32]().Bind(value)
	return scalarParameter(codec.TypeInteger, v, err)
}

type integerNumber interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}
