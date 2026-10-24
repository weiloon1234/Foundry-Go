package extensionstore

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Translation struct {
	Key        string
	Owner      string
	Scope      string
	SubjectKey string
	Identity   value.JSON[model.Identity]
	Field      string
	Locale     string
	Value      string
	// CreatedAt preserves explicit input; persistence supplies the owning clock when omitted.
	CreatedAt temporal.DateTime
	// UpdatedAt is refreshed from the owning clock after before-write hooks and before mutators.
	UpdatedAt temporal.DateTime
}

func (Translation) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("model translation row")) }
