package authtransport

import "fmt"

// TicketResponse delivers a single-use handshake ticket. Like TokenResponse it
// intentionally discloses its credential to the JSON encoder only.
//
//foundry:dto
type TicketResponse struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int64  `json:"expires_in"`
}

func (TicketResponse) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("ticket response")) }
