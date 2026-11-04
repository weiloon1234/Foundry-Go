package error_details

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type RegistryDetails struct {
	ExistingRegistryID string `json:"existing_registry_id"`
}

type SeatDetails struct {
	SeatID     string `json:"seat_id"`
	RetryAfter int64  `json:"retry_after"`
}

func Failures() [2]*h.DetailedError {
	registry := h.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.DefineJSON[RegistryDetails](contract.Schema{
		Root: "foundry.test/consumer/testdata/footprint/error_details.RegistryDetails",
		Types: []contract.Type{
			{ID: "foundry.test/consumer/testdata/footprint/error_details.RegistryDetails", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "existing_registry_id", Type: "string", Required: true}}},
			{ID: "string", Kind: contract.StringKind},
		},
	}))
	seat := h.DefineErrorDetails("seat_unavailable", 409, "Seat is unavailable.", contract.DefineJSON[SeatDetails](contract.Schema{
		Root: "foundry.test/consumer/testdata/footprint/error_details.SeatDetails",
		Types: []contract.Type{
			{ID: "foundry.test/consumer/testdata/footprint/error_details.SeatDetails", Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "seat_id", Type: "string", Required: true},
				{Name: "retry_after", Type: "int64", Required: true},
			}},
			{ID: "string", Kind: contract.StringKind},
			{ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		},
	}))
	return [2]*h.DetailedError{registry.New(RegistryDetails{ExistingRegistryID: "registry-123"}).WithCause(nil), seat.New(SeatDetails{SeatID: "seat-123", RetryAfter: 30}).WithCause(nil)}
}
