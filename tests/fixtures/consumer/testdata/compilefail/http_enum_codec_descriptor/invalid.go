package compilefail

import (
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type First string
type Second string

func (v First) MarshalText() ([]byte, error)     { return []byte(v), nil }
func (v *First) UnmarshalText(data []byte) error { *v = First(data); return nil }

var _ = foundryhttp.EnumPath[First, *First](enum.Describe("app", "Second", enum.Case[Second]{Name: "Ready", Value: "ready"}))
