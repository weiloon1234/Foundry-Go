package invalid

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type Created struct{}
type RegistryExistsDetails struct {
	ExistingRegistryID string `json:"existing_registry_id"`
}
type OtherDetails struct {
	ExistingRegistryID string `json:"existing_registry_id"`
}

func invalid(descriptor contract.JSON[RegistryExistsDetails]) (Created, error) {
	RegistryExists := h.DefineErrorDetails("registry_exists", 409, "Registry already exists.", descriptor)
	return Created{}, RegistryExists.New(OtherDetails{ExistingRegistryID: "registry-123"})
}
