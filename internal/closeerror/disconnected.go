// Package closeerror recognizes disconnected transports only during physical close.
package closeerror

import (
	"io"
	"net"
	"slices"
	"syscall"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// OnlyDisconnected requires every leaf to identify a disconnected connection.
// protocol may recognize additional protocol-specific leaves. Unknown failures,
// classified faults and incomplete inspection remain failures. Never use this
// to classify queries, commands, transactions, or shutdown wait deadlines.
func OnlyDisconnected(err error, protocol func(error) bool) bool {
	if err == nil {
		return false
	}
	closed, other := false, false
	inspected := callback.Isolated("classify connection close", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			switch current {
			case io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, net.ErrClosed, syscall.ECONNRESET, syscall.EPIPE:
				closed = true
				return true
			}
			if protocol != nil && protocol(current) {
				closed = true
				return true
			}
			switch current := current.(type) {
			case *fault.Error:
				other = true
			case interface{ Unwrap() error }:
				other = current.Unwrap() == nil
			case interface{ Unwrap() []error }:
				other = !slices.ContainsFunc(current.Unwrap(), func(child error) bool { return child != nil })
			default:
				other = true
			}
			return !other
		})
		other = other || !complete
		return nil
	})
	return inspected == nil && closed && !other
}
