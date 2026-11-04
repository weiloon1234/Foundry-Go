package error_details

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	h "github.com/weiloon1234/Foundry-Go/http"
)

type RegistryID string
type SeatID string

func Failures() [2]*h.DetailedError {
	registry := h.DefineErrorDetails("registry_exists", 409, "Registry already exists.", contract.StringJSON[RegistryID]())
	seat := h.DefineErrorDetails("seat_unavailable", 409, "Seat is unavailable.", contract.StringJSON[SeatID]())
	return [2]*h.DetailedError{registry.New("registry-123").WithCause(nil), seat.New("seat-123").WithCause(nil)}
}
