package authtransport

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

//foundry:dto
type RefreshRequest struct {
	RefreshToken RefreshCredential `json:"refresh_token"`
}

// RefreshCredential validates opaque token syntax at JSON input. It remains
// redacted when formatted or re-serialized. Secret is its explicit use boundary.
type RefreshCredential struct{ secret secret.String }

func (v RefreshCredential) Secret() secret.String      { return v.secret }
func (RefreshCredential) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte(secret.Redacted)) }
func (RefreshCredential) LogValue() slog.Value         { return slog.StringValue(secret.Redacted) }
func (RefreshCredential) MarshalJSON() ([]byte, error) { return json.Marshal(secret.Redacted) }
func (v *RefreshCredential) UnmarshalJSON(data []byte) error {
	if v == nil {
		return fault.New(fault.Invalid, "refresh credential destination is missing")
	}
	*v = RefreshCredential{}
	candidate, err := parseTokenCredential(data)
	if err != nil {
		return err
	}
	v.secret = candidate
	return nil
}

func (RefreshCredential) JSONContract() contract.JSON[RefreshCredential] {
	const name contract.TypeID = "github.com/weiloon1234/Foundry-Go/internal/authtransport.RefreshCredential"
	return contract.DefineJSONValue[RefreshCredential](contract.Schema{
		Root: name, Types: []contract.Type{{ID: name, Kind: contract.StringKind}},
	})
}
