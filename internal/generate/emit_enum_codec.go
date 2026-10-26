package generate

import (
	"fmt"
	"go/types"
)

func (e *emitter) enumCodec(declaration enum) string {
	codec := e.use(framework + "/database/codec")
	class := "Signed"
	if declaration.base.Kind() == types.String {
		class = "String"
	} else if declaration.base.Info()&types.IsUnsigned != 0 {
		class = "Unsigned"
	}
	name := e.typeName(declaration.typ)
	return fmt.Sprintf("%s.%s[%s]().Validated(%s.Validate)", codec, class, name, name)
}
